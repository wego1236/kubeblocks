/*
Copyright ApeCloud, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dbaasv1alpha1 "github.com/apecloud/kubeblocks/apis/dbaas/v1alpha1"
	"github.com/apecloud/kubeblocks/internal/cli/create"
	"github.com/apecloud/kubeblocks/internal/cli/delete"
	"github.com/apecloud/kubeblocks/internal/cli/types"
	"github.com/apecloud/kubeblocks/internal/cli/util"
	cfgcore "github.com/apecloud/kubeblocks/internal/configuration"
)

type OperationsOptions struct {
	create.BaseOptions
	ComponentNames         []string `json:"componentNames,omitempty"`
	OpsRequestName         string   `json:"opsRequestName"`
	TTLSecondsAfterSucceed int      `json:"ttlSecondsAfterSucceed"`

	// OpsType operation type
	OpsType dbaasv1alpha1.OpsType `json:"type"`

	// OpsTypeLower lower OpsType
	OpsTypeLower string `json:"typeLower"`

	// Upgrade options
	ClusterVersionRef string `json:"clusterVersionRef"`

	// VerticalScaling options
	RequestCPU    string `json:"requestCPU"`
	RequestMemory string `json:"requestMemory"`
	LimitCPU      string `json:"limitCPU"`
	LimitMemory   string `json:"limitMemory"`

	// HorizontalScaling options
	Replicas int `json:"replicas"`

	// Reconfiguring options
	URLPath         string            `json:"urlPath"`
	Parameters      []string          `json:"parameters"`
	KeyValues       map[string]string `json:"keyValues"`
	CfgTemplateName string            `json:"cfgTemplateName"`
	CfgFile         string            `json:"cfgFile"`

	// VolumeExpansion options.
	// VCTNames VolumeClaimTemplate names
	VCTNames []string `json:"vctNames,omitempty"`
	Storage  string   `json:"storage"`
}

func newBaseOperationsOptions(streams genericclioptions.IOStreams, opsType dbaasv1alpha1.OpsType) *OperationsOptions {
	return &OperationsOptions{
		BaseOptions: create.BaseOptions{IOStreams: streams},
		OpsType:     opsType,
		// nil cannot be set to a map struct in CueLang, so init the map of KeyValues.
		KeyValues: map[string]string{},
	}
}

var (
	createReconfigureExample = templates.Examples(`
		# update component params 
		kbcli cluster configure <cluster-name> --component-name=<component-name> --template-name=<template-name> --configure-file=<configure-file> --set max_connections=1000,general_log=OFF

		# update mysql max_connections, cluster name is mycluster
		kbcli cluster configure mycluster --component-name=mysql --template-name=mysql-3node-tpl --configure-file=my.cnf --set max_connections=2000
	`)
)

// buildCommonFlags build common flags for operations command
func (o *OperationsOptions) buildCommonFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.OpsRequestName, "ops-request", "", "OpsRequest name. if not specified, it will be randomly generated ")
	cmd.Flags().IntVar(&o.TTLSecondsAfterSucceed, "ttlSecondsAfterSucceed", 0, "Time to live after the OpsRequest succeed")
	if o.OpsType != dbaasv1alpha1.UpgradeType && o.OpsType != dbaasv1alpha1.ReconfiguringType {
		cmd.Flags().StringSliceVar(&o.ComponentNames, "component-names", nil, " Component names to this operations")
	}
}

// CompleteRestartOps when restart a cluster and component-names is null, represents restarting the entire cluster.
// we should set all component names to ComponentNames
func (o *OperationsOptions) CompleteRestartOps() error {
	if len(o.ComponentNames) != 0 {
		return nil
	}
	gvr := schema.GroupVersionResource{Group: types.Group, Version: types.Version, Resource: types.ResourceClusters}
	if unstructuredObj, err := o.Client.Resource(gvr).Namespace(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{}); err != nil {
		return err
	} else {
		if o.ComponentNames, _, err = unstructured.NestedStringSlice(unstructuredObj.Object, "status", "operations", "restartable"); err != nil {
			return err
		}
	}
	return nil
}

func (o *OperationsOptions) validateUpgrade() error {
	if len(o.ClusterVersionRef) == 0 {
		return fmt.Errorf("missing cluster-version")
	}
	return delete.Confirm([]string{o.Name}, o.In)
}

func (o *OperationsOptions) validateVolumeExpansion() error {
	if len(o.VCTNames) == 0 {
		return fmt.Errorf("missing volume-claim-template-names")
	}
	if len(o.Storage) == 0 {
		return fmt.Errorf("missing storage")
	}
	return nil
}

func (o *OperationsOptions) validateHorizontalScaling() error {
	if o.Replicas < -1 {
		return fmt.Errorf("replicas required natural number")
	}
	return nil
}

func (o *OperationsOptions) validateReconfiguring() error {
	if len(o.ComponentNames) != 1 {
		return cfgcore.MakeError("reconfiguring only support one component.")
	}
	if len(o.URLPath) != 0 {
		if _, err := os.Stat(o.URLPath); err != nil {
			return cfgcore.WrapError(err, "failed to check if %s exists", o.URLPath)
		}
		return nil
	}

	if err := o.validateUpdatedParams(); err != nil {
		return cfgcore.WrapError(err, "failed to validate updated params.")
	}

	componentName := o.ComponentNames[0]
	tplList, err := util.GetConfigTemplateList(o.Name, o.Namespace, o.Client, componentName)
	if err != nil {
		return err
	}
	tpl, err := o.validateTemplateParam(tplList)
	if err != nil {
		return err
	}
	if err := o.validateConfigMapKey(tpl, componentName); err != nil {
		return err
	}
	if err := o.validateConfigParams(tpl, componentName); err != nil {
		return err
	}
	return nil
}

func (o *OperationsOptions) validateConfigParams(tpl *dbaasv1alpha1.ConfigTemplate, componentName string) error {
	var (
		configConstraint = dbaasv1alpha1.ConfigConstraint{}
	)

	transKeyPair := func(pts map[string]string) map[string]interface{} {
		m := make(map[string]interface{}, len(pts))
		for key, value := range pts {
			m[key] = value
		}
		return m
	}

	if err := util.GetResourceObjectFromGVR(types.ConfigConstraintGVR(), client.ObjectKey{
		Namespace: "",
		Name:      tpl.ConfigConstraintRef,
	}, o.Client, &configConstraint); err != nil {
		return err
	}

	_, err := cfgcore.MergeAndValidateConfiguration(configConstraint.Spec, map[string]string{o.CfgFile: ""}, []cfgcore.ParamPairs{{
		Key:           o.CfgFile,
		UpdatedParams: transKeyPair(o.KeyValues),
	}})
	return err
}

func (o *OperationsOptions) validateTemplateParam(tpls []dbaasv1alpha1.ConfigTemplate) (*dbaasv1alpha1.ConfigTemplate, error) {
	if len(tpls) == 0 {
		return nil, cfgcore.MakeError("not support reconfiguring because there is no config template.")
	}

	if len(o.CfgTemplateName) == 0 && len(tpls) > 1 {
		return nil, cfgcore.MakeError("when multi templates exist, must specify which template to use.")
	}

	// Autofill Config template name.
	if len(o.CfgTemplateName) == 0 && len(tpls) == 1 {
		tpl := &tpls[0]
		o.CfgTemplateName = tpl.Name
		return tpl, nil
	}

	for i := range tpls {
		tpl := &tpls[i]
		if tpl.Name == o.CfgTemplateName {
			return tpl, nil
		}
	}
	return nil, cfgcore.MakeError("specify template name[%s] is not exist.", o.CfgTemplateName)
}

func (o *OperationsOptions) validateConfigMapKey(tpl *dbaasv1alpha1.ConfigTemplate, componentName string) error {
	var (
		cmObj  = corev1.ConfigMap{}
		cmName = cfgcore.GetComponentCfgName(o.Name, componentName, tpl.VolumeName)
	)

	if err := util.GetResourceObjectFromGVR(types.CMGVR(), client.ObjectKey{
		Name:      cmName,
		Namespace: o.Namespace,
	}, o.Client, &cmObj); err != nil {
		return err
	}
	if len(cmObj.Data) == 0 {
		return cfgcore.MakeError("not support reconfiguring because there is no config file.")
	}

	// Autofill ConfigMap key
	if len(o.CfgFile) == 0 && len(cmObj.Data) == 1 {
		for k := range cmObj.Data {
			o.CfgFile = k
			return nil
		}
	}
	if _, ok := cmObj.Data[o.CfgFile]; !ok {
		return cfgcore.MakeError("specify file name[%s] is not exist.", o.CfgFile)
	}
	return nil
}

func (o *OperationsOptions) validateUpdatedParams() error {
	if len(o.Parameters) == 0 && len(o.URLPath) == 0 {
		return cfgcore.MakeError("reconfiguring required configure file or updated parameters.")
	}

	o.KeyValues = make(map[string]string)
	for _, param := range o.Parameters {
		pp := strings.Split(param, ",")
		for _, p := range pp {
			fields := strings.SplitN(p, "=", 2)
			if len(fields) != 2 {
				return cfgcore.MakeError("updated parameter formatter: key=value")
			}
			o.KeyValues[fields[0]] = fields[1]
		}
	}
	return nil
}

// Validate command flags or args is legal
func (o *OperationsOptions) Validate() error {
	if o.Name == "" {
		return fmt.Errorf("missing cluster name")
	}

	if o.OpsType == dbaasv1alpha1.UpgradeType {
		return o.validateUpgrade()
	}

	// common validate for componentOps
	if len(o.ComponentNames) == 0 {
		return fmt.Errorf("missing component-names")
	}

	switch o.OpsType {
	case dbaasv1alpha1.VolumeExpansionType:
		if err := o.validateVolumeExpansion(); err != nil {
			return err
		}
	case dbaasv1alpha1.HorizontalScalingType:
		if err := o.validateHorizontalScaling(); err != nil {
			return err
		}
	case dbaasv1alpha1.ReconfiguringType:
		if err := o.validateReconfiguring(); err != nil {
			return err
		}
	}
	return delete.Confirm([]string{o.Name}, o.In)
}

func (o *OperationsOptions) fillComponentNameForReconfiguring() error {
	if len(o.ComponentNames) != 0 {
		return nil
	}

	componentNames, err := util.GetComponentsFromClusterCR(client.ObjectKey{
		Namespace: o.Namespace,
		Name:      o.Name,
	}, o.Client)
	if err != nil {
		return err
	}
	if len(componentNames) != 1 {
		return cfgcore.MakeError("when multi component exist, must specify which component to use.")
	}
	o.ComponentNames = componentNames
	return nil
}

// buildOperationsInputs build operations inputs
func buildOperationsInputs(f cmdutil.Factory, o *OperationsOptions) create.Inputs {
	o.OpsTypeLower = strings.ToLower(string(o.OpsType))
	return create.Inputs{
		CueTemplateName: "cluster_operations_template.cue",
		ResourceName:    types.ResourceOpsRequests,
		BaseOptionsObj:  &o.BaseOptions,
		Options:         o,
		Factory:         f,
		Validate:        o.Validate,
	}
}

var restartExample = templates.Examples(`
		# restart all components
		kbcli cluster restart <my-cluster>

		# restart specifies the component, separate with commas when <component-name> more than one
		kbcli cluster restart <my-cluster> --component-names=<component-name>
`)

// NewRestartCmd create a restart command
func NewRestartCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.RestartType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "restart"
	inputs.Short = "Restart the specified components in the cluster"
	inputs.Example = restartExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
	}
	inputs.Complete = o.CompleteRestartOps
	return create.BuildCommand(inputs)
}

var upgradeExample = templates.Examples(`
		# upgrade the cluster to the specified version 
		kbcli cluster upgrade <my-cluster> --cluster-version=<cluster-version>
`)

// NewUpgradeCmd create a upgrade command
func NewUpgradeCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.UpgradeType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "upgrade"
	inputs.Short = "Upgrade the cluster version"
	inputs.Example = upgradeExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
		cmd.Flags().StringVar(&o.ClusterVersionRef, "cluster-version", "", "Reference cluster version (required)")
	}
	return create.BuildCommand(inputs)
}

var verticalScalingExample = templates.Examples(`
		# scale the computing resources of specified components, separate with commas when <component-name> more than one
		kbcli cluster vscale <my-cluster> --component-names=<component-name> --requests.cpu=500m \
        --requests.memory=500Mi --limits.cpu=500m --limits.memory=500Mi
`)

// NewVerticalScalingCmd create a vertical scaling command
func NewVerticalScalingCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.VerticalScalingType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "vscale"
	inputs.Short = "Vertically scale the specified components in the cluster"
	inputs.Example = verticalScalingExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
		cmd.Flags().StringVar(&o.RequestCPU, "requests.cpu", "", "CPU size requested by the component")
		cmd.Flags().StringVar(&o.RequestMemory, "requests.memory", "", "Memory size requested by the component")
		cmd.Flags().StringVar(&o.LimitCPU, "limits.cpu", "", "CPU size limited by the component")
		cmd.Flags().StringVar(&o.LimitMemory, "limits.memory", "", "Memory size limited by the component")
	}
	return create.BuildCommand(inputs)
}

var horizontalScalingExample = templates.Examples(`
		# expand storage resources of specified components, separate with commas when <component-name> more than one
		kbcli cluster hscale <my-cluster> --component-names=<component-name> --replicas=3
`)

// NewHorizontalScalingCmd create a horizontal scaling command
func NewHorizontalScalingCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.HorizontalScalingType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "hscale"
	inputs.Short = "Horizontally scale the specified components in the cluster"
	inputs.Example = horizontalScalingExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
		cmd.Flags().IntVar(&o.Replicas, "replicas", -1, "Replicas with the specified components")
	}
	return create.BuildCommand(inputs)
}

var volumeExpansionExample = templates.Examples(`
		# restart specifies the component, separate with commas when <component-name> more than one
		kbcli cluster volume-expand <my-cluster> --component-names=<component-name> \ 
  		--volume-claim-template-names=data --storage=10Gi
`)

// NewVolumeExpansionCmd create a vertical scaling command
func NewVolumeExpansionCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.VolumeExpansionType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "volume-expand"
	inputs.Short = "Expand volume with the specified components and volumeClaimTemplates in the cluster"
	inputs.Example = volumeExpansionExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
		cmd.Flags().StringSliceVar(&o.VCTNames, "volume-claim-template-names", nil, "VolumeClaimTemplate names in components (required)")
		cmd.Flags().StringVar(&o.Storage, "storage", "", "Volume storage size (required)")
	}
	return create.BuildCommand(inputs)
}

// NewReconfigureCmd create a Reconfiguring command
func NewReconfigureCmd(f cmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := newBaseOperationsOptions(streams, dbaasv1alpha1.ReconfiguringType)
	inputs := buildOperationsInputs(f, o)
	inputs.Use = "configure"
	inputs.Short = "reconfigure parameters with the specified components in the cluster"
	inputs.Example = createReconfigureExample
	inputs.BuildFlags = func(cmd *cobra.Command) {
		o.buildCommonFlags(cmd)
		cmd.Flags().StringSliceVar(&o.Parameters, "set", nil, "Specify updated parameter list. For details about the parameters, refer to kbcli sub command: 'kbcli cluster describe-configure'.")
		cmd.Flags().StringSliceVar(&o.ComponentNames, "component-name", nil, "Specify the name of Component to be updated. If the cluster has only one component, unset the parameter.")
		cmd.Flags().StringVar(&o.CfgTemplateName, "template-name", "", "Specify the name of the configuration template to be updated (e.g. for apecloud-mysql: --template-name=mysql-3node-tpl). What templates or configure files are available for this cluster can refer to kbcli sub command: 'kbcli cluster describe-configure'.")
		cmd.Flags().StringVar(&o.CfgFile, "configure-file", "", "Specify the name of the configuration file to be updated (e.g. for mysql: --configure-file=my.cnf). What templates or configure files are available for this cluster can refer to kbcli sub command: 'kbcli cluster describe-configure'.")
	}
	inputs.Complete = o.fillComponentNameForReconfiguring
	return create.BuildCommand(inputs)
}
