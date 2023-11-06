/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"serverless.alipay.com/sofa-serverless/arkctl/common/cmdutil"
	"serverless.alipay.com/sofa-serverless/arkctl/common/contextutil"
	"serverless.alipay.com/sofa-serverless/arkctl/common/fileutil"
	"serverless.alipay.com/sofa-serverless/arkctl/v1/cmd/root"
	"serverless.alipay.com/sofa-serverless/arkctl/v1/service/ark"

	"github.com/spf13/cobra"
)

var (
	buildFlag  string
	bundleFlag string
	portFlag   int
	podFlag    string

	doLocalBuildBundle = false
)

const (
	ctxKeyArkService              = "ark.Service"
	ctxKeyBizModel                = "ark.BizModel"
	ctxKeyArkContainerRuntimeInfo = "ark.ContainerRuntimeInfo"
)

var DeployCommand = &cobra.Command{
	Use: "deploy",
	Short: `
"deploy your biz module to running containers."
`,
	Long: `
The arkctl deploy command can help you quickly deploy your biz module to running containers.
We advice you to use this in local dev phase.
`,
	Example: `
The arkctl deploy command can help you quickly deploy your biz module to running containers.
Here is some scenarios and corresponding commands:

Scenario 0: Build Bundle and Deploy It to local running ark container at current directory with default port:
    arkctl deploy

Scenario 1: Build Bundle and Deploy It to local running ark container:
	arkctl deploy --build ${path/to/your/project} --port ${your ark container portFlag} 

Scenario 2: Deploy a local bundleFlag to local running ark container:
	arkctl deploy --bundle ${path/to/your/project} --port ${your ark container portFlag} 

Scenario 3: Deploy a local bundleFlag to remote ark container running in k8s podFlag:
!!Make sure you already have kubectl and exec permission to the k8s cluster in your working environment!!.
	arkctl deploy --bundle ${url/to/your/bundle} --pod ${namespace}/${name} --dir ${bundle dir inside port} -port ${your ark container port}
`,
	ValidArgs:         nil,
	ValidArgsFunction: nil,
	Args: func(cmd *cobra.Command, args []string) error {
		// if bundle is provided, then there is no need to build
		doLocalBuildBundle = !cmd.Flags().Changed("bundle")

		// if bundle is not provided and build is not provided as well, set to current directory
		if doLocalBuildBundle && !cmd.Flags().Changed("build") {
			buildFlag, _ = os.Getwd()
		}

		return nil
	},
	Run: executeDeploy,
}

func execMavenBuild(ctx *contextutil.Context) bool {
	logger := contextutil.GetLogger(ctx)
	if !doLocalBuildBundle {
		logger.Info("build bundle skipped!")
	}

	mvn := cmdutil.BuildCommandWithWorkDir(
		ctx,
		buildFlag,
		"mvn",
		"clean", "package", "-Dmaven.test.skip=true")

	logger.WithField("dir", buildFlag).Info("start to build bundle.")
	if err := mvn.Exec(); err != nil {
		logger.WithError(err).Error("build bundle failed!")
	}

	go func() {
		for line := range mvn.Output() {
			fmt.Println(line)
		}
	}()

	if err := <-mvn.Wait(); err != nil {
		logger.WithError(err).Error("build bundle failed!")
		return false
	}

	if err := mvn.GetExitError(); err != nil {
		logger.WithError(err).Error("build bundle failed!")
		return false
	}

	return true
}

func execParseBizModel(ctx *contextutil.Context) bool {
	var (
		logger = contextutil.GetLogger(ctx)
	)

	bundlePath := bundleFlag
	if doLocalBuildBundle {
		searchdir := buildFlag
		if searchdir == "" {
			searchdir, _ = os.Getwd()
		}

		filepath.Walk(searchdir, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() && strings.HasSuffix(info.Name(), "-ark-biz.jar") {
				bundlePath = path
			}
			return nil
		})

		if !strings.HasSuffix(bundlePath, "-ark-biz.jar") {
			logger.Error("can not find pre built biz bundle in build dir!")
			return false
		}
		bundlePath = "file://" + bundlePath
	}

	bizModel, err := ark.ParseBizModel(ctx, fileutil.FileUrl(bundlePath))
	if err != nil {
		logger.WithError(err).Error("parse biz model failed!")
		return false
	}

	ctx.Put(ctxKeyBizModel, bizModel)

	return true
}

// uninstall the given package in target ark container
func execUnInstall(ctx *contextutil.Context) bool {
	var (
		arkService              = ctx.Value(ctxKeyArkService).(ark.Service)
		bizModel                = ctx.Value(ctxKeyBizModel).(*ark.BizModel)
		arkContainerRuntimeInfo = ctx.Value(ctxKeyArkContainerRuntimeInfo).(*ark.ArkContainerRuntimeInfo)
		logger                  = contextutil.GetLogger(ctx)
	)

	logger.WithField("port", portFlag).
		WithField("bizName", bizModel.BizName).
		WithField("bizVersion", bizModel.BizVersion).
		WithField("runType", arkContainerRuntimeInfo.RunType).
		Info("start to uninstall bundle.")

	if err := arkService.UnInstallBiz(ctx, ark.UnInstallBizRequest{
		BizModel:        *bizModel,
		TargetContainer: *arkContainerRuntimeInfo,
	}); err != nil {
		logger.WithError(err).Error("uninstall biz failed!")
		return false
	}

	logger.Info("uninstall biz success!")
	return true
}

// install the given package in target ark container
func execInstall(ctx *contextutil.Context) bool {
	var (
		arkService              = ctx.Value(ctxKeyArkService).(ark.Service)
		bizModel                = ctx.Value(ctxKeyBizModel).(*ark.BizModel)
		arkContainerRuntimeInfo = ctx.Value(ctxKeyArkContainerRuntimeInfo).(*ark.ArkContainerRuntimeInfo)
		logger                  = contextutil.GetLogger(ctx)
	)

	logger.WithField("port", portFlag).
		WithField("bizName", bizModel.BizName).
		WithField("bizVersion", bizModel.BizVersion).
		WithField("runType", arkContainerRuntimeInfo.RunType).
		Info("start to install bundle.")

	if err := arkService.InstallBiz(ctx, ark.InstallBizRequest{
		BizModel:        *bizModel,
		TargetContainer: *arkContainerRuntimeInfo,
	}); err != nil {
		logger.WithError(err).Error("install biz failed!")
		return false
	}
	logger.Info("install biz success!")
	return true
}

func generateContext(cmd *cobra.Command) *contextutil.Context {
	ctx := contextutil.NewContext(context.Background())

	arkService := ark.BuildService(ctx)
	ctx.Put(ctxKeyArkService, arkService)

	arkContainerRuntimeInfo := &ark.ArkContainerRuntimeInfo{
		RunType: ark.ArkContainerRunTypeLocal,
		Port:    &portFlag,
	}

	// target is running inside of kubernetes
	if podFlag != "" {
		arkContainerRuntimeInfo.RunType = ark.ArkContainerRunTypeK8s
		arkContainerRuntimeInfo.Coordinate = podFlag
	}
	ctx.Put(ctxKeyArkContainerRuntimeInfo, arkContainerRuntimeInfo)

	return ctx
}

// executeDeploy will execute the deploy command
// 1. build the biz bundle
// 2. parse the biz model for further usage
// 3. uninstall the biz bundle in target ark container to prevent conflict
// 4. install the biz bundle in target ark container
func executeDeploy(cobracmd *cobra.Command, _ []string) {
	c := generateContext(cobracmd)

	todos := []func(context2 *contextutil.Context) bool{
		execMavenBuild,
		execParseBizModel,
		execUnInstall,
		execInstall,
	}

	for _, todo := range todos {
		if !todo(c) {
			return
		}
	}

}

func init() {
	root.RootCmd.AddCommand(DeployCommand)
	DeployCommand.Flags().StringVar(&buildFlag, "build", "", `
Build the project at given directory and then deploy it to running containers.
If not provided, arkctl will try to buildFlag the project in current directory.
`)

	DeployCommand.Flags().StringVar(&bundleFlag, "bundle", "", `
Provide the pre-built bundleFlag url and then deploy it to running containers.
If not provided, arkctl will try to find the bundleFlag in current directory.
`)

	DeployCommand.Flags().StringVar(&podFlag, "pod", "", `
If Provided, arkctl will try to deploy the bundleFlag to the given podFlag instead of local running process.
`)

	DeployCommand.Flags().IntVar(&portFlag, "port", 1238, `
The default portFlag of ark container is 1238.
`)

}
