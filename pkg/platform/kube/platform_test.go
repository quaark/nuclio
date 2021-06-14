// +build test_unit

/*
Copyright 2017 The Nuclio Authors.

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

package kube

import (
	"fmt"
	"testing"

	"github.com/nuclio/nuclio/pkg/common"
	"github.com/nuclio/nuclio/pkg/containerimagebuilderpusher"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/opa"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/platform/abstract"
	"github.com/nuclio/nuclio/pkg/platform/kube/apis/nuclio.io/v1beta1"
	"github.com/nuclio/nuclio/pkg/platform/kube/client"
	"github.com/nuclio/nuclio/pkg/platform/kube/client/clientset/mocks"
	"github.com/nuclio/nuclio/pkg/platform/kube/ingress"
	mockplatform "github.com/nuclio/nuclio/pkg/platform/mock"
	"github.com/nuclio/nuclio/pkg/platformconfig"

	"github.com/google/go-cmp/cmp"
	"github.com/nuclio/errors"
	"github.com/nuclio/logger"
	"github.com/nuclio/zap"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

type KubePlatformTestSuite struct {
	suite.Suite
	mockedPlatform                *mockplatform.Platform
	nuclioioV1beta1InterfaceMock  *mocks.NuclioV1beta1Interface
	nuclioFunctionInterfaceMock   *mocks.NuclioFunctionInterface
	nuclioAPIGatewayInterfaceMock *mocks.NuclioAPIGatewayInterface
	nuclioioInterfaceMock         *mocks.Interface
	kubeClientSet                 fake.Clientset
	abstractPlatform              *abstract.Platform
	Namespace                     string
	Logger                        logger.Logger
	Platform                      *Platform
	PlatformKubeConfig            *platformconfig.PlatformKubeConfig
	mockedOpaClient               *opa.MockClient
}

func (suite *KubePlatformTestSuite) SetupSuite() {
	var err error

	common.SetVersionFromEnv()

	suite.Namespace = "default-namespace"
	suite.Logger, err = nucliozap.NewNuclioZapTest("test")
	suite.Require().NoError(err, "Logger should create successfully")

	suite.PlatformKubeConfig = &platformconfig.PlatformKubeConfig{
		DefaultServiceType: v1.ServiceTypeClusterIP,
	}
	suite.mockedPlatform = &mockplatform.Platform{}
	abstractPlatform, err := abstract.NewPlatform(suite.Logger, suite.mockedPlatform, &platformconfig.Config{
		Kube: *suite.PlatformKubeConfig,
	}, "")
	suite.Require().NoError(err, "Could not create platform")

	abstractPlatform.ContainerBuilder, err = containerimagebuilderpusher.NewNop(suite.Logger, nil)
	suite.Require().NoError(err)
	suite.abstractPlatform = abstractPlatform
	suite.mockedOpaClient = &opa.MockClient{}
	suite.abstractPlatform.OpaClient = suite.mockedOpaClient
	suite.kubeClientSet = *fake.NewSimpleClientset()
}

func (suite *KubePlatformTestSuite) SetupTest() {
	suite.resetCRDMocks()
}

func (suite *KubePlatformTestSuite) resetCRDMocks() {
	suite.nuclioioInterfaceMock = &mocks.Interface{}
	suite.nuclioioV1beta1InterfaceMock = &mocks.NuclioV1beta1Interface{}
	suite.nuclioFunctionInterfaceMock = &mocks.NuclioFunctionInterface{}
	suite.nuclioAPIGatewayInterfaceMock = &mocks.NuclioAPIGatewayInterface{}

	suite.nuclioioInterfaceMock.
		On("NuclioV1beta1").
		Return(suite.nuclioioV1beta1InterfaceMock)
	suite.nuclioioV1beta1InterfaceMock.
		On("NuclioFunctions", suite.Namespace).
		Return(suite.nuclioFunctionInterfaceMock)
	suite.nuclioioV1beta1InterfaceMock.
		On("NuclioAPIGateways", suite.Namespace).
		Return(suite.nuclioAPIGatewayInterfaceMock)

	getter, err := client.NewGetter(suite.Logger, suite.Platform)
	suite.Require().NoError(err)

	suite.Platform = &Platform{
		Platform: suite.abstractPlatform,
		getter:   getter,
		consumer: &client.Consumer{
			NuclioClientSet: suite.nuclioioInterfaceMock,
			KubeClientSet:   &suite.kubeClientSet,
		},
	}
	suite.Platform.updater, _ = client.NewUpdater(suite.Logger, suite.Platform.consumer, suite.Platform)
	suite.Platform.deleter, _ = client.NewDeleter(suite.Logger, suite.Platform)
}

type FunctionKubePlatformTestSuite struct {
	KubePlatformTestSuite
}

func (suite *FunctionKubePlatformTestSuite) TestFunctionTriggersEnrichmentAndValidation() {

	// return empty api gateways list on enrichFunctionsWithAPIGateways (not tested here)
	suite.nuclioAPIGatewayInterfaceMock.
		On("List", metav1.ListOptions{}).
		Return(&v1beta1.NuclioAPIGatewayList{}, nil)

	for idx, testCase := range []struct {
		name                     string
		setUpFunction            func() error
		tearDownFunction         func() error
		triggers                 map[string]functionconfig.Trigger
		expectedEnrichedTriggers map[string]functionconfig.Trigger

		// keep empty when no error is expected
		validationError string
	}{
		{
			name:     "EnrichWithDefaultHTTPTrigger",
			triggers: nil,
			expectedEnrichedTriggers: func() map[string]functionconfig.Trigger {
				defaultHTTPTrigger := functionconfig.GetDefaultHTTPTrigger()
				defaultHTTPTrigger.Attributes = map[string]interface{}{
					"serviceType": suite.PlatformKubeConfig.DefaultServiceType,
				}
				return map[string]functionconfig.Trigger{
					defaultHTTPTrigger.Name: defaultHTTPTrigger,
				}
			}(),
		},
		{
			name: "PathIsAvailable",
			setUpFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-name",
						Namespace: suite.Namespace,
					},
					Spec: extensionsv1beta1.IngressSpec{
						Rules: []extensionsv1beta1.IngressRule{
							{
								Host: "host-and-path-already-in-use.com",
								IngressRuleValue: extensionsv1beta1.IngressRuleValue{
									HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
										Paths: []extensionsv1beta1.HTTPIngressPath{
											{
												Path: "used-path/",
											},
										},
									},
								},
							},
						},
					},
				})
				return nil
			},
			tearDownFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset()
				return nil
			},
			triggers: map[string]functionconfig.Trigger{
				"http-with-ingress": {
					Kind: "http",
					Attributes: map[string]interface{}{
						"ingresses": map[string]interface{}{
							"0": map[string]interface{}{
								"host":  "host-and-path-already-in-use.com",
								"paths": []string{"/unused-path"},
							},
						},
					},
				},
			},
		},
		{
			name: "FailPathInUse",
			setUpFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-name",
						Namespace: suite.Namespace,
					},
					Spec: extensionsv1beta1.IngressSpec{
						Rules: []extensionsv1beta1.IngressRule{
							{
								Host: "host-and-path-already-in-use.com",
								IngressRuleValue: extensionsv1beta1.IngressRuleValue{
									HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
										Paths: []extensionsv1beta1.HTTPIngressPath{
											{
												Path: "used-path/",
											},
										},
									},
								},
							},
						},
					},
				})
				return nil
			},
			tearDownFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset()
				return nil
			},
			triggers: map[string]functionconfig.Trigger{
				"http-with-ingress": {
					Kind: "http",
					Attributes: map[string]interface{}{
						"ingresses": map[string]interface{}{
							"0": map[string]interface{}{
								"host":  "host-and-path-already-in-use.com",
								"paths": []string{"/unused-path", "used-path/"},
							},
						},
					},
				},
			},
			validationError: platform.ErrIngressHostPathInUse.Error(),
		},
	} {
		suite.Run(testCase.name, func() {

			// run test case specific set up function if given
			if testCase.setUpFunction != nil {
				err := testCase.setUpFunction()
				suite.Require().NoError(err)
			}

			// mock get projects
			suite.mockedPlatform.On("GetProjects", &platform.GetProjectsOptions{
				Meta: platform.ProjectMeta{
					Name:      platform.DefaultProjectName,
					Namespace: suite.Namespace,
				},
			}).Return([]platform.Project{
				&platform.AbstractProject{},
			}, nil).Once()

			// name it with index and shift with 65 to get A as first letter
			functionName := string(rune(idx + 65))
			functionConfig := *functionconfig.NewConfig()

			createFunctionOptions := &platform.CreateFunctionOptions{
				Logger:         suite.Logger,
				FunctionConfig: functionConfig,
			}
			createFunctionOptions.FunctionConfig.Meta.Name = functionName
			createFunctionOptions.FunctionConfig.Meta.Namespace = suite.Namespace
			createFunctionOptions.FunctionConfig.Meta.Labels = map[string]string{
				"nuclio.io/project-name": platform.DefaultProjectName,
			}
			createFunctionOptions.FunctionConfig.Spec.Triggers = testCase.triggers
			suite.Logger.DebugWith("Enriching and validating function", "functionName", functionName)

			// run enrichment
			err := suite.Platform.EnrichFunctionConfig(&createFunctionOptions.FunctionConfig)
			suite.Require().NoError(err, "Failed to enrich function")

			if testCase.expectedEnrichedTriggers != nil {
				suite.Equal(testCase.expectedEnrichedTriggers,
					createFunctionOptions.FunctionConfig.Spec.Triggers)
			}

			// run validation
			err = suite.Platform.ValidateFunctionConfig(&createFunctionOptions.FunctionConfig)
			if testCase.validationError != "" {
				suite.Require().Error(err, "Validation passed unexpectedly")
				suite.Require().Equal(testCase.validationError, errors.RootCause(err).Error())
			} else {
				suite.Require().NoError(err, "Validation failed unexpectedly")
			}

			// run test case specific tear down function if given
			if testCase.tearDownFunction != nil {
				err := testCase.tearDownFunction()
				suite.Require().NoError(err)
			}
		})
	}
}

func (suite *FunctionKubePlatformTestSuite) TestGetFunctionInstanceAndConfig() {
	for _, testCase := range []struct {
		name                    string
		functionName            string
		hasAPIGateways          bool
		apiGateWayName          string
		expectValidationFailure bool
		functionExists          bool
	}{
		{
			name:         "functionNotFound",
			functionName: "not-found",
		},
		{
			name:           "functionFound",
			functionName:   "found-me",
			functionExists: true,
		},
		{
			name:           "functionFoundEnrichedWithAPIGateways",
			functionName:   "found-me",
			hasAPIGateways: true,
			apiGateWayName: "api-gw-name",
			functionExists: true,
		},
	} {
		suite.Run(testCase.name, func() {
			var getFunctionResponseErr error
			var getFunctionResponse *v1beta1.NuclioFunction
			listAPIGatewayResponse := v1beta1.NuclioAPIGatewayList{Items: []v1beta1.NuclioAPIGateway{}}

			// prepare mock responses
			if !testCase.functionExists {
				getFunctionResponseErr = apierrors.NewNotFound(schema.GroupResource{}, "asd")
			} else {
				getFunctionResponse = &v1beta1.NuclioFunction{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: suite.Namespace,
						Name:      testCase.functionName,
					},
				}
			}

			if testCase.hasAPIGateways {
				listAPIGatewayResponse.Items = append(listAPIGatewayResponse.Items,
					v1beta1.NuclioAPIGateway{
						ObjectMeta: metav1.ObjectMeta{
							Name: testCase.apiGateWayName,
						},
						Spec: platform.APIGatewaySpec{
							Upstreams: []platform.APIGatewayUpstreamSpec{
								{
									Kind: platform.APIGatewayUpstreamKindNuclioFunction,
									Nucliofunction: &platform.NuclioFunctionAPIGatewaySpec{
										Name: testCase.functionName,
									},
								},
							},
						},
					})
			}

			suite.nuclioFunctionInterfaceMock.
				On("Get", testCase.functionName, metav1.GetOptions{}).
				Return(getFunctionResponse, getFunctionResponseErr).
				Once()
			defer suite.nuclioAPIGatewayInterfaceMock.AssertExpectations(suite.T())

			if testCase.functionExists {
				suite.nuclioAPIGatewayInterfaceMock.
					On("List", metav1.ListOptions{}).
					Return(&listAPIGatewayResponse, nil).
					Once()
				defer suite.nuclioAPIGatewayInterfaceMock.AssertExpectations(suite.T())
			}

			functionInstance, functionConfigAndStatus, err := suite.Platform.
				getFunctionInstanceAndConfig(suite.Namespace,
					testCase.functionName,
					true)

			if testCase.expectValidationFailure {
				suite.Require().Error(err)
				return
			}

			// no error
			suite.Require().NoError(err)

			// response might be nil, if function was not found
			if !testCase.functionExists {
				suite.Require().Nil(functionInstance)
				suite.Require().Nil(functionConfigAndStatus)
				return
			}

			// ensure found function matches its function name input
			suite.Require().Equal(functionInstance.Name, testCase.functionName)
			suite.Require().Equal(functionConfigAndStatus.Meta.Name, testCase.functionName)
			suite.Require().Empty(cmp.Diff(functionInstance.Spec, functionConfigAndStatus.Spec))
			suite.Require().Empty(cmp.Diff(functionInstance.Status, functionConfigAndStatus.Status))

			if testCase.hasAPIGateways {
				suite.Require().Contains(functionConfigAndStatus.Status.APIGateways, testCase.apiGateWayName)
			}
		})
	}
}

func (suite *FunctionKubePlatformTestSuite) TestGetFunctionsPermissions() {
	var getFunctionResponse *v1beta1.NuclioFunction

	for _, testCase := range []struct {
		name           string
		opaResponse    bool
		givenMemberIds bool
		raiseForbidden bool
	}{
		{
			name:           "Allowed",
			opaResponse:    true,
			givenMemberIds: true,
			raiseForbidden: true,
		},
		{
			name:           "Forbidden with Error",
			opaResponse:    false,
			givenMemberIds: true,
			raiseForbidden: true,
		},
		{
			name:           "Forbidden no Error",
			opaResponse:    false,
			givenMemberIds: true,
			raiseForbidden: false,
		},
		{
			name:           "No OPA",
			opaResponse:    false,
			givenMemberIds: false,
			raiseForbidden: false,
		},
	} {
		suite.Run(testCase.name, func() {
			var memberIds []string

			functionName := "func"
			projectName := "proj"

			getFunctionResponse = &v1beta1.NuclioFunction{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: suite.Namespace,
					Name:      functionName,
					Labels: map[string]string{
						"nuclio.io/project-name": projectName,
					},
				},
			}

			suite.nuclioFunctionInterfaceMock.
				On("Get", functionName, metav1.GetOptions{}).
				Return(getFunctionResponse, nil).
				Once()
			defer suite.nuclioFunctionInterfaceMock.AssertExpectations(suite.T())

			if testCase.givenMemberIds {
				memberIds = []string{"id1", "id2"}

				suite.mockedOpaClient.
					On("QueryPermissions",
						fmt.Sprintf("/projects/%s/functions/%s", projectName, functionName),
						opa.ActionRead,
						memberIds).
					Return(testCase.opaResponse, nil).
					Once()
				defer suite.mockedOpaClient.AssertExpectations(suite.T())
			}
			functions, err := suite.Platform.GetFunctions(&platform.GetFunctionsOptions{
				Name:           functionName,
				Namespace:      suite.Namespace,
				MemberIds:      memberIds,
				RaiseForbidden: testCase.raiseForbidden,
			})

			if !testCase.opaResponse && testCase.givenMemberIds {
				if testCase.raiseForbidden {
					suite.Assert().Error(err)
				} else {
					suite.Assert().NoError(err)
					suite.Assert().Equal(0, len(functions))
				}
			} else {
				suite.Assert().NoError(err)
				suite.Assert().Equal(1, len(functions))
				suite.Assert().Equal(functionName, functions[0].GetConfig().Meta.Name)
			}
		})
	}
}

func (suite *FunctionKubePlatformTestSuite) TestUpdateFunctionPermissions() {
	var getFunctionResponse *v1beta1.NuclioFunction

	for _, testCase := range []struct {
		name        string
		opaResponse bool
	}{
		{
			name:        "Allowed",
			opaResponse: true,
		},
		{
			name:        "Forbidden",
			opaResponse: false,
		},
	} {
		suite.Run(testCase.name, func() {
			functionName := "func"
			projectName := "proj"
			memberIds := []string{"id1", "id2"}

			getFunctionResponse = &v1beta1.NuclioFunction{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: suite.Namespace,
					Name:      functionName,
					Labels: map[string]string{
						"nuclio.io/project-name": projectName,
					},
				},
				Status: functionconfig.Status{
					State: functionconfig.FunctionStateReady,
				},
			}

			suite.nuclioFunctionInterfaceMock.
				On("Get", functionName, metav1.GetOptions{}).
				Return(getFunctionResponse, nil).
				Once()
			defer suite.nuclioFunctionInterfaceMock.AssertExpectations(suite.T())

			suite.mockedOpaClient.
				On("QueryPermissions",
					fmt.Sprintf("/projects/%s/functions/%s", projectName, functionName),
					opa.ActionUpdate,
					memberIds).
				Return(testCase.opaResponse, nil).
				Once()
			defer suite.mockedOpaClient.AssertExpectations(suite.T())

			if testCase.opaResponse {

				// verify
				verifyUpdateFunction := func(function *v1beta1.NuclioFunction) bool {
					suite.Require().Equal(functionName, function.Name)
					suite.Require().Equal(suite.Namespace, function.Namespace)

					return true
				}

				suite.nuclioFunctionInterfaceMock.
					On("Get", functionName, metav1.GetOptions{}).
					Return(getFunctionResponse, nil).
					Once()

				suite.nuclioFunctionInterfaceMock.
					On("Update", mock.MatchedBy(verifyUpdateFunction)).
					Return(getFunctionResponse, nil).
					Once()
				defer suite.nuclioFunctionInterfaceMock.AssertExpectations(suite.T())
			}

			err := suite.Platform.UpdateFunction(&platform.UpdateFunctionOptions{
				FunctionMeta: &functionconfig.Meta{
					Name:      functionName,
					Namespace: suite.Namespace,
				},
				MemberIds: memberIds,
			})

			if !testCase.opaResponse {
				suite.Assert().Error(err)
			} else {
				suite.Assert().NoError(err)
			}
		})
	}
}

func (suite *FunctionKubePlatformTestSuite) TestDeleteFunctionPermissions() {
	for _, testCase := range []struct {
		name        string
		opaResponse bool
	}{
		{
			name:        "Allowed",
			opaResponse: true,
		},
		{
			name:        "Forbidden",
			opaResponse: false,
		},
	} {
		suite.Run(testCase.name, func() {
			functionName := "func"
			projectName := "proj"
			memberIds := []string{"id1", "id2"}

			returnedFunction := platform.AbstractFunction{
				Config: functionconfig.Config{
					Meta: functionconfig.Meta{
						Namespace: suite.Namespace,
						Name:      functionName,
						Labels: map[string]string{
							"nuclio.io/project-name": projectName,
						},
					},
				},
			}

			suite.mockedPlatform.
				On("GetFunctions", &platform.GetFunctionsOptions{
					Name:      functionName,
					Namespace: suite.Namespace,
				}).
				Return([]platform.Function{&returnedFunction}, nil).
				Once()
			defer suite.mockedPlatform.AssertExpectations(suite.T())

			suite.mockedOpaClient.
				On("QueryPermissions",
					fmt.Sprintf("/projects/%s/functions/%s", projectName, functionName),
					opa.ActionDelete,
					memberIds).
				Return(testCase.opaResponse, nil).
				Once()
			defer suite.mockedOpaClient.AssertExpectations(suite.T())

			if testCase.opaResponse {
				suite.nuclioAPIGatewayInterfaceMock.
					On("List", metav1.ListOptions{}).
					Return(&v1beta1.NuclioAPIGatewayList{}, nil).
					Once()
				defer suite.nuclioAPIGatewayInterfaceMock.AssertExpectations(suite.T())

				suite.nuclioFunctionInterfaceMock.
					On("Delete", functionName, &metav1.DeleteOptions{}).
					Return(nil).
					Once()
				defer suite.nuclioFunctionInterfaceMock.AssertExpectations(suite.T())
			}

			err := suite.Platform.DeleteFunction(&platform.DeleteFunctionOptions{
				FunctionConfig: functionconfig.Config{
					Meta: functionconfig.Meta{
						Name:      functionName,
						Namespace: suite.Namespace,
					},
				},
				MemberIds: memberIds,
			})

			if !testCase.opaResponse {
				suite.Assert().Error(err)
			} else {
				suite.Assert().NoError(err)
			}
		})
	}
}

type APIGatewayKubePlatformTestSuite struct {
	KubePlatformTestSuite
}

func (suite *APIGatewayKubePlatformTestSuite) TestAPIGatewayEnrichmentAndValidation() {

	// return empty api gateways list on enrichFunctionsWithAPIGateways (not tested here)
	suite.nuclioAPIGatewayInterfaceMock.
		On("List", metav1.ListOptions{}).
		Return(&v1beta1.NuclioAPIGatewayList{}, nil)

	for _, testCase := range []struct {
		name             string
		setUpFunction    func() error
		tearDownFunction func() error
		apiGatewayConfig *platform.APIGatewayConfig

		// keep empty to skip the enrichment validation
		expectedEnrichedAPIGateway *platform.APIGatewayConfig

		// the matching api gateway upstream functions
		upstreamFunctions []*v1beta1.NuclioFunction

		// keep empty when shouldn't fail
		validationError string
	}{
		{
			name: "SpecNameEnrichedFromMetaName",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Name = ""
				apiGatewayConfig.Meta.Name = "meta-name"
				return &apiGatewayConfig
			}(),
			expectedEnrichedAPIGateway: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Name = "meta-name"
				apiGatewayConfig.Meta.Name = "meta-name"
				return &apiGatewayConfig
			}(),
		},
		{
			name: "MetaNameEnrichedFromSpecName",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Name = "spec-name"
				apiGatewayConfig.Meta.Name = ""
				return &apiGatewayConfig
			}(),
			expectedEnrichedAPIGateway: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Name = "spec-name"
				apiGatewayConfig.Meta.Name = "spec-name"
				return &apiGatewayConfig
			}(),
		},
		{
			name: "ValidateNamespaceExistence",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Meta.Namespace = ""
				return &apiGatewayConfig
			}(),
			validationError: "Api gateway namespace must be provided in metadata",
		},
		{
			name: "ValidateNameExistence",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Meta.Name = ""
				apiGatewayConfig.Spec.Name = ""
				return &apiGatewayConfig
			}(),
			validationError: "Api gateway name must be provided in metadata",
		},
		{
			name: "ValidateNameEqualsInSpecAndMeta",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Meta.Name = "name1"
				apiGatewayConfig.Spec.Name = "name2"
				return &apiGatewayConfig
			}(),
			validationError: "Api gateway metadata.name must match api gateway spec.name",
		},
		{
			name: "ValidateNoReservedName",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Meta.Name = "dashboard"
				apiGatewayConfig.Spec.Name = "dashboard"
				return &apiGatewayConfig
			}(),
			validationError: "Api gateway name 'dashboard' is reserved and cannot be used",
		},
		{
			name: "ValidateNoMoreThanTwoUpstreams",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				upstream := apiGatewayConfig.Spec.Upstreams[0]
				apiGatewayConfig.Spec.Upstreams = []platform.APIGatewayUpstreamSpec{upstream, upstream, upstream}
				return &apiGatewayConfig
			}(),
			validationError: "Received more than 2 upstreams. Currently not supported",
		},
		{
			name: "ValidateAtLeastOneUpstream",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Upstreams = []platform.APIGatewayUpstreamSpec{}
				return &apiGatewayConfig
			}(),
			validationError: "One or more upstreams must be provided in spec",
		},
		{
			name: "ValidateHostExistence",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Host = ""
				return &apiGatewayConfig
			}(),
			validationError: "Host must be provided in spec",
		},
		{
			name: "ValidateUpstreamKind",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Upstreams[0].Kind = "bad-kind"
				return &apiGatewayConfig
			}(),
			validationError: "Unsupported upstream kind: 'bad-kind'. (Currently supporting only nucliofunction)",
		},
		{
			name: "ValidateAllUpstreamsHaveSameKind",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				differentKindUpstream := apiGatewayConfig.Spec.Upstreams[0]
				differentKindUpstream.Kind = "kind-2"
				apiGatewayConfig.Spec.Upstreams = append(apiGatewayConfig.Spec.Upstreams, differentKindUpstream)
				return &apiGatewayConfig
			}(),
			validationError: "All upstreams must be of the same kind",
		},
		{
			name: "ValidateAPIGatewayFunctionHasNoIngresses",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Upstreams[0].Nucliofunction.Name = "function-with-ingresses"
				return &apiGatewayConfig
			}(),
			upstreamFunctions: []*v1beta1.NuclioFunction{
				{
					Spec: functionconfig.Spec{
						Triggers: map[string]functionconfig.Trigger{
							"http-with-ingress": {
								Kind: "http",
								Attributes: map[string]interface{}{
									"ingresses": map[string]interface{}{
										"0": map[string]interface{}{
											"host":  "some-host",
											"paths": []string{"/"},
										},
									},
								},
							},
						},
					},
				},
			},
			validationError: "Api gateway upstream function: function-with-ingresses must not have an ingress",
		},
		{
			name: "ValidateAPIGatewayCanaryFunctionHasNoIngresses",
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Upstreams[0].Nucliofunction.Name = "function-without-ingresses"
				apiGatewayConfig.Spec.Upstreams = append(apiGatewayConfig.Spec.Upstreams, platform.APIGatewayUpstreamSpec{
					Kind: platform.APIGatewayUpstreamKindNuclioFunction,
					Nucliofunction: &platform.NuclioFunctionAPIGatewaySpec{
						Name: "function-with-ingresses-2",
					},
				})
				return &apiGatewayConfig
			}(),
			upstreamFunctions: []*v1beta1.NuclioFunction{
				{}, // primary upstream function is empty (has no ingresses)
				{
					Spec: functionconfig.Spec{
						Triggers: map[string]functionconfig.Trigger{
							"http-with-ingress": {
								Kind: "http",
								Attributes: map[string]interface{}{
									"ingresses": map[string]interface{}{
										"0": map[string]interface{}{
											"host":  "some-host",
											"paths": []string{"/"},
										},
									},
								},
							},
						},
					},
				},
			},
			validationError: "Api gateway upstream function: function-with-ingresses-2 must not have an ingress",
		},
		{
			name: "PathIsAvailable",
			setUpFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-name",
						Namespace: suite.Namespace,
					},
					Spec: extensionsv1beta1.IngressSpec{
						Rules: []extensionsv1beta1.IngressRule{
							{
								Host: "this-host-and-path-are-used.com",
								IngressRuleValue: extensionsv1beta1.IngressRuleValue{
									HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
										Paths: []extensionsv1beta1.HTTPIngressPath{
											{
												Path: "different-path/",
											},
										},
									},
								},
							},
						},
					},
				})
				return nil
			},
			tearDownFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset()
				return nil
			},
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Host = "this-host-and-path-are-used.com"
				apiGatewayConfig.Spec.Path = "//same-path"
				return &apiGatewayConfig
			}(),
		},
		{
			name: "FailPathInUse",
			setUpFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-name",
						Namespace: suite.Namespace,
					},
					Spec: extensionsv1beta1.IngressSpec{
						Rules: []extensionsv1beta1.IngressRule{
							{
								Host: "this-host-and-path-are-used.com",
								IngressRuleValue: extensionsv1beta1.IngressRuleValue{
									HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
										Paths: []extensionsv1beta1.HTTPIngressPath{
											{
												Path: "same-path/",
											},
										},
									},
								},
							},
						},
					},
				})
				return nil
			},
			tearDownFunction: func() error {
				suite.kubeClientSet = *fake.NewSimpleClientset()
				return nil
			},
			apiGatewayConfig: func() *platform.APIGatewayConfig {
				apiGatewayConfig := suite.compileAPIGatewayConfig()
				apiGatewayConfig.Spec.Host = "this-host-and-path-are-used.com"
				apiGatewayConfig.Spec.Path = "//same-path"
				return &apiGatewayConfig
			}(),
			validationError: platform.ErrIngressHostPathInUse.Error(),
		},
	} {
		suite.Run(testCase.name, func() {
			if testCase.expectedEnrichedAPIGateway != nil {
				if testCase.expectedEnrichedAPIGateway.Meta.Labels == nil {
					testCase.expectedEnrichedAPIGateway.Meta.Labels = map[string]string{}
				}
				suite.Platform.EnrichLabelsWithProjectName(testCase.expectedEnrichedAPIGateway.Meta.Labels)
			}

			// run test case specific set up function if given
			if testCase.setUpFunction != nil {
				err := testCase.setUpFunction()
				suite.Require().NoError(err)
			}

			// run enrichment
			suite.Platform.EnrichAPIGatewayConfig(testCase.apiGatewayConfig)
			if testCase.expectedEnrichedAPIGateway != nil {
				suite.Require().Empty(cmp.Diff(testCase.expectedEnrichedAPIGateway, testCase.apiGatewayConfig))
			}

			// mock Get functions, when iterating over upstreams on validateAPIGatewayFunctionsHaveNoIngresses
			for idx, upstream := range testCase.apiGatewayConfig.Spec.Upstreams {
				var upstreamFunction *v1beta1.NuclioFunction
				var getFunctionsError interface{}

				if len(testCase.upstreamFunctions) > idx {
					upstreamFunction = testCase.upstreamFunctions[idx]
					getFunctionsError = nil
				} else {

					// return no function if not specified (not found)
					upstreamFunction = &v1beta1.NuclioFunction{}
					getFunctionsError = &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonNotFound}}
				}

				suite.nuclioFunctionInterfaceMock.
					On("Get", upstream.Nucliofunction.Name, metav1.GetOptions{}).
					Return(upstreamFunction, getFunctionsError).
					Once()
			}

			// run validation
			err := suite.Platform.ValidateAPIGatewayConfig(testCase.apiGatewayConfig)
			if testCase.validationError != "" {
				suite.Assert().Error(err)
				suite.Assert().Equal(testCase.validationError, errors.RootCause(err).Error())
			} else {
				suite.Assert().NoError(err)
			}

			// run test case specific tear down function if given
			if testCase.tearDownFunction != nil {
				err := testCase.tearDownFunction()
				suite.Require().NoError(err)
			}
		})
	}
}

func (suite *APIGatewayKubePlatformTestSuite) TestAPIGatewayUpdate() {

	// return empty api gateways list on enrichFunctionsWithAPIGateways (not tested here)
	suite.nuclioAPIGatewayInterfaceMock.
		On("List", metav1.ListOptions{}).
		Return(&v1beta1.NuclioAPIGatewayList{}, nil)

	for _, testCase := range []struct {
		name                    string
		updateAPIGatewayOptions func(baseAPIGatewayConfig *platform.APIGatewayConfig) *platform.UpdateAPIGatewayOptions
	}{
		{
			name: "UpdateFields",
			updateAPIGatewayOptions: func(baseAPIGatewayConfig *platform.APIGatewayConfig) *platform.UpdateAPIGatewayOptions {
				updateAPIGatewayOptions := &platform.UpdateAPIGatewayOptions{
					APIGatewayConfig: &platform.APIGatewayConfig{
						Meta:   baseAPIGatewayConfig.Meta,
						Spec:   baseAPIGatewayConfig.Spec,
						Status: baseAPIGatewayConfig.Status,
					},
				}
				// modify a field
				updateAPIGatewayOptions.APIGatewayConfig.Spec.Host = "update-me.com"
				updateAPIGatewayOptions.APIGatewayConfig.Meta.Labels = map[string]string{
					"newLabel": "label-value",
				}
				updateAPIGatewayOptions.APIGatewayConfig.Meta.Annotations = map[string]string{
					"newAnnotation": "annotation-value",
				}
				return updateAPIGatewayOptions
			},
		},
	} {
		suite.Run(testCase.name, func() {
			apiGatewayConfig := suite.compileAPIGatewayConfig()
			updateAPIGatewayOptions := testCase.updateAPIGatewayOptions(&apiGatewayConfig)

			// get before update
			suite.nuclioAPIGatewayInterfaceMock.
				On("Get", apiGatewayConfig.Meta.Name, metav1.GetOptions{}).
				Return(&v1beta1.NuclioAPIGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apiGatewayConfig.Meta.Name,
						Namespace: apiGatewayConfig.Meta.Namespace,
					},
					Spec:   apiGatewayConfig.Spec,
					Status: apiGatewayConfig.Status,
				}, nil).
				Once()

			verifyAPIGatewayToUpdate := func(apiGatewayToUpdate *v1beta1.NuclioAPIGateway) bool {
				suite.Require().Empty(cmp.Diff(updateAPIGatewayOptions.APIGatewayConfig.Spec, apiGatewayToUpdate.Spec))
				suite.Require().Empty(cmp.Diff(updateAPIGatewayOptions.APIGatewayConfig.Meta.Annotations, apiGatewayToUpdate.Annotations))
				suite.Require().Empty(cmp.Diff(updateAPIGatewayOptions.APIGatewayConfig.Meta.Labels, apiGatewayToUpdate.Labels))
				suite.Require().Equal(platform.APIGatewayStateWaitingForProvisioning, apiGatewayToUpdate.Status.State)
				return true
			}

			// mock kubernetes update
			suite.nuclioAPIGatewayInterfaceMock.
				On("Update", mock.MatchedBy(verifyAPIGatewayToUpdate)).
				Return(func(apiGateway *v1beta1.NuclioAPIGateway) *v1beta1.NuclioAPIGateway {

					// nothing really to do here, let Kubernetes do the actual upgrade
					return apiGateway
				}, nil).
				Once()

			// no function with matching upstreams
			suite.nuclioFunctionInterfaceMock.
				On("Get", apiGatewayConfig.Spec.Upstreams[0].Nucliofunction.Name, metav1.GetOptions{}).
				Return(nil,
					&apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonNotFound}}).
				Once()

			// update
			err := suite.Platform.UpdateAPIGateway(updateAPIGatewayOptions)
			suite.Require().NoError(err)
		})
	}
}

func (suite *APIGatewayKubePlatformTestSuite) compileAPIGatewayConfig() platform.APIGatewayConfig {
	return platform.APIGatewayConfig{
		Meta: platform.APIGatewayMeta{
			Name:      "default-name",
			Namespace: suite.Namespace,
		},
		Spec: platform.APIGatewaySpec{
			Name:               "default-name",
			Host:               "default-host",
			AuthenticationMode: ingress.AuthenticationModeNone,
			Upstreams: []platform.APIGatewayUpstreamSpec{
				{
					Kind: platform.APIGatewayUpstreamKindNuclioFunction,
					Nucliofunction: &platform.NuclioFunctionAPIGatewaySpec{
						Name: "default-func-name",
					},
				},
			},
		},
		Status: platform.APIGatewayStatus{
			State: platform.APIGatewayStateWaitingForProvisioning,
		},
	}
}

func TestKubePlatformTestSuite(t *testing.T) {
	suite.Run(t, new(FunctionKubePlatformTestSuite))
	suite.Run(t, new(APIGatewayKubePlatformTestSuite))
}
