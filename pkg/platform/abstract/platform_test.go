package abstract

import (
	"testing"

	"github.com/nuclio/nuclio/pkg/dockerclient"
	"github.com/nuclio/nuclio/pkg/functionconfig"
	"github.com/nuclio/nuclio/pkg/platform"
	"github.com/nuclio/nuclio/pkg/version"

	"github.com/nuclio/logger"
	nucliozap "github.com/nuclio/zap"
	"github.com/rs/xid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type TestPlatform struct {
	platform.Platform
	logger         logger.Logger
	suiteAssertion *assert.Assertions
}

// GetProjects will list existing projects
func (mp *TestPlatform) GetProjects(getProjectsOptions *platform.GetProjectsOptions) ([]platform.Project, error) {
	project, err := platform.NewAbstractProject(mp.logger, nil, platform.ProjectConfig{})
	mp.suiteAssertion.NoError(err, "Failed to create new abstract project")
	return []platform.Project{
		project,
	}, nil
}

type TestAbstractSuite struct {
	suite.Suite
	Logger           logger.Logger
	DockerClient     dockerclient.Client
	Platform         *Platform
	TestID           string
	Runtime          string
	RuntimeDir       string
	FunctionDir      string
	containerID      string
	TempDir          string
	CleanupTemp      bool
	DefaultNamespace string
}

func (suite *TestAbstractSuite) SetupSuite() {
	err := version.Set(&version.Info{
		GitCommit: "c",
		Label:     "latest",
		Arch:      "amd64",
		OS:        "linux",
	})
	suite.Require().NoError(err, "Failed to set version info")

	suite.DefaultNamespace = "nuclio"

	suite.Logger, err = nucliozap.NewNuclioZapTest("test")
	suite.Require().NoError(err, "Logger should create successfully")

	suite.DockerClient, err = dockerclient.NewShellClient(suite.Logger, nil)
	suite.Require().NoError(err, "Docker client should create successfully")

	testPlatform := &TestPlatform{
		logger:         suite.Logger,
		suiteAssertion: suite.Assert(),
	}
	suite.Platform, err = NewPlatform(suite.Logger, testPlatform, nil)
	suite.NoError(err, "Could not create platform")
}

func (suite *TestAbstractSuite) SetupTest() {
	suite.TestID = xid.New().String()
}

// Test function with invalid min max replicas
func (suite *TestAbstractSuite) TestMinMaxReplicas() {
	zero := 0
	one := 1
	two := 2
	for idx, MinMaxReplicas := range []struct {
		MinReplicas          *int
		MaxReplicas          *int
		ExpectedMinReplicas  *int
		ExpectedMaxReplicas  *int
		shouldFailValidation bool
	}{
		{MinReplicas: nil, MaxReplicas: nil, ExpectedMinReplicas: nil, ExpectedMaxReplicas: nil, shouldFailValidation: false},
		{MinReplicas: nil, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: nil, MaxReplicas: &one, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: nil, MaxReplicas: &two, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &zero, MaxReplicas: nil, shouldFailValidation: true},
		{MinReplicas: &zero, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &zero, MaxReplicas: &one, ExpectedMinReplicas: &zero, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &zero, MaxReplicas: &two, ExpectedMinReplicas: &zero, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &one, MaxReplicas: nil, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &one, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &one, MaxReplicas: &one, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &one, shouldFailValidation: false},
		{MinReplicas: &one, MaxReplicas: &two, ExpectedMinReplicas: &one, ExpectedMaxReplicas: &two, shouldFailValidation: false},

		{MinReplicas: &two, MaxReplicas: nil, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},
		{MinReplicas: &two, MaxReplicas: &zero, shouldFailValidation: true},
		{MinReplicas: &two, MaxReplicas: &one, shouldFailValidation: true},
		{MinReplicas: &two, MaxReplicas: &two, ExpectedMinReplicas: &two, ExpectedMaxReplicas: &two, shouldFailValidation: false},
	} {

		// name it with index and shift with 65 to get A as first letter
		functionName := string(idx + 65)
		functionConfig := *functionconfig.NewConfig()

		createFunctionOptions := &platform.CreateFunctionOptions{
			Logger:         suite.Logger,
			FunctionConfig: functionConfig,
		}

		createFunctionOptions.FunctionConfig.Meta.Name = functionName
		createFunctionOptions.FunctionConfig.Meta.Labels = map[string]string{
			"nuclio.io/project-name": platform.DefaultProjectName,
		}
		createFunctionOptions.FunctionConfig.Spec.MinReplicas = MinMaxReplicas.MinReplicas
		createFunctionOptions.FunctionConfig.Spec.MaxReplicas = MinMaxReplicas.MaxReplicas
		suite.Logger.DebugWith("Checking function ", "functionName", functionName)

		err := suite.Platform.EnrichCreateFunctionOptions(createFunctionOptions)
		suite.NoError(err, "Failed to enrich function")

		err = suite.Platform.ValidateCreateFunctionOptions(createFunctionOptions)
		if MinMaxReplicas.shouldFailValidation {
			suite.Error(err, "Validation should fail")
			suite.Logger.DebugWith("Validation failed as expected ", "functionName", functionName)
			continue
		}
		suite.NoError(err, "Failed to validate function")
		functionConfigSpec := createFunctionOptions.FunctionConfig.Spec

		if MinMaxReplicas.ExpectedMinReplicas != nil {
			suite.Assert().Equal(*MinMaxReplicas.ExpectedMinReplicas, *functionConfigSpec.MinReplicas)
		} else {
			suite.Assert().Nil(functionConfigSpec.MinReplicas)
		}

		if MinMaxReplicas.ExpectedMaxReplicas != nil {
			suite.Assert().Equal(*MinMaxReplicas.ExpectedMaxReplicas, *functionConfigSpec.MaxReplicas)
		} else {
			suite.Assert().Nil(functionConfigSpec.MaxReplicas)
		}
		suite.Logger.DebugWith("Validation passed successfully", "functionName", functionName)
	}
}

func TestAbstractPlatformTestSuite(t *testing.T) {
	if testing.Short() {
		return
	}

	suite.Run(t, new(TestAbstractSuite))
}
