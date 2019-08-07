package run

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	oct "github.com/kyma-incubator/octopus/pkg/apis/testing/v1alpha1"
	"github.com/kyma-project/cli/internal/kube"
	"github.com/kyma-project/cli/pkg/api/octopus"
	"github.com/kyma-project/cli/pkg/kyma/cmd/test"
	"github.com/kyma-project/cli/pkg/kyma/core"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type command struct {
	opts *options
	core.Command
}

func NewCmd(o *options) *cobra.Command {
	cmd := command{
		Command: core.Command{Options: o.Options},
		opts:    o,
	}

	cobraCmd := &cobra.Command{
		Use:   "run <test-definition-1> <test-defintion-2> ... <test-definition-N>",
		Short: "Runs tests on a Kyma cluster",
		Long: `Runs tests on a Kyma cluster

If you don't provide any specific test definitions, all available test definitions will be added to the newly created test suite.
If you don't specify the value for the -n flag, the name of the test suite name will be autogenerated.

To execute all test defintions, run the"example-test" test suite:
kyma test run -n example-test
`,
		RunE:    func(_ *cobra.Command, args []string) error { return cmd.Run(args) },
		Aliases: []string{"r"},
	}

	cobraCmd.Flags().StringVarP(&o.Name, "name", "n", "", "Name of the new test suite")
	cobraCmd.Flags().Int64VarP(&o.ExecutionCount, "count", "c", 1, "Number of execution rounds for each test in the suite. You cannot configure this value in parallel with max-retries")
	cobraCmd.Flags().Int64VarP(&o.MaxRetries, "max-retries", "", 1, "Number of retries for a failed test.")
	cobraCmd.Flags().Int64VarP(&o.Concurrency, "concurrency", "", 1, "Number of tests to be executed in parallel.")
	return cobraCmd
}

func (cmd *command) Run(args []string) error {
	var err error
	if cmd.K8s, err = kube.NewFromConfig("", cmd.KubeconfigPath); err != nil {
		return errors.Wrap(err, "Could not initialize the Kubernetes client. Make sure your kubeconfig is valid.")
	}

	var testSuiteName string
	if len(cmd.opts.Name) > 0 {
		testSuiteName = cmd.opts.Name
	} else {
		rand.Seed(time.Now().UTC().UnixNano())
		rnd := rand.Int31()
		testSuiteName = fmt.Sprintf("test-%d", rnd)
	}

	tNotExists, err := verifyIfTestNotExists(testSuiteName, cmd.K8s.Octopus())
	if err != nil {
		return err
	}
	if !tNotExists {
		return fmt.Errorf("Test suite '%s' already exists", testSuiteName)
	}

	clusterTestDefs, err := cmd.K8s.Octopus().ListTestDefinitions()
	if err != nil {
		return errors.Wrap(err, "Unable to get the list of test definitions")
	}

	var testDefToApply []oct.TestDefinition
	if len(args) == 0 {
		testDefToApply = clusterTestDefs.Items
	} else {
		if testDefToApply, err = matchTestDefinitionNames(args,
			clusterTestDefs.Items); err != nil {
			return err
		}
	}

	testResource := generateTestsResource(testSuiteName,
		cmd.opts.ExecutionCount, cmd.opts.MaxRetries,
		cmd.opts.Concurrency, testDefToApply)
	if err != nil {
		return err
	}

	if err := cmd.K8s.Octopus().CreateTestSuite(testResource); err != nil {
		return err
	}

	fmt.Printf("test suite '%s' successfully created\r\n", testSuiteName)
	return nil
}

func matchTestDefinitionNames(testNames []string,
	testDefs []oct.TestDefinition) ([]oct.TestDefinition, error) {
	result := []oct.TestDefinition{}
	for _, tName := range testNames {
		found := false
		for _, tDef := range testDefs {
			if strings.ToLower(tName) == strings.ToLower(tDef.GetName()) {
				found = true
				result = append(result, tDef)
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Test defintion '%s' not found in the list of cluster test definitions", tName)
		}
	}
	return result, nil
}

func generateTestsResource(testName string, numberOfExecutions,
	maxRetries, concurrency int64,
	testDefinitions []oct.TestDefinition) *oct.ClusterTestSuite {

	octTestDefs := test.NewTestSuite(testName)
	matchNames := []oct.TestDefReference{}
	for _, td := range testDefinitions {
		matchNames = append(matchNames, oct.TestDefReference{
			Name:      td.GetName(),
			Namespace: td.GetNamespace(),
		})
	}
	octTestDefs.Spec.MaxRetries = maxRetries
	octTestDefs.Spec.Concurrency = concurrency
	octTestDefs.Spec.Count = numberOfExecutions
	octTestDefs.Spec.Selectors.MatchNames = matchNames

	return octTestDefs
}

func listTestSuiteNames(cli octopus.OctopusInterface) ([]string, error) {
	suites, err := cli.ListTestSuites()
	if err != nil {
		return nil, errors.Wrap(err, "Unable to list test suites")
	}

	var result = make([]string, len(suites.Items))
	for i := 0; i < len(suites.Items); i++ {
		result[i] = suites.Items[i].GetName()
	}
	return result, nil
}

func verifyIfTestNotExists(suiteName string,
	cli octopus.OctopusInterface) (bool, error) {
	tests, err := listTestSuiteNames(cli)
	if err != nil {
		return false, err
	}
	for _, t := range tests {
		if t == suiteName {
			return false, nil
		}
	}
	return true, nil
}