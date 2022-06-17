package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mt-sre/addon-metadata-operator/internal/cli"
	"github.com/mt-sre/addon-metadata-operator/pkg/types"
	"github.com/mt-sre/addon-metadata-operator/pkg/utils"
	"github.com/mt-sre/addon-metadata-operator/pkg/validator"
	_ "github.com/mt-sre/addon-metadata-operator/pkg/validator/register"
	"github.com/spf13/cobra"
)

const long = "Validate an addon metadata and it's bundles against custom validators."

func examples() string {
	return strings.Join([]string{
		"  # Validate an addon in staging. Uses the latest version if it supports imageset.",
		"  mtcli validate --env stage --version latest internal/testdata/addons-imageset/reference-addon",
		"  # Validate a version 1.0.0 of a production addon using imageset.",
		"  mtcli validate --env production --version 1.0.0 <path/to/addon_dir>",
		"  # Validate a staging addon that is not using imageset, but a static indexImage.",
		"  mtcli validate --env stage <path/to/addon_dir>",
		"  # Validate an integration addon using imageset, disabling validators 001_foo and 002_bar.",
		"  mtcli validate --env integration --disabled AM0001,AM0002 <path/to/addon_dir>",
		"  # Validate an integration addon using imageset, enabled only 001_foo.",
		"  mtcli validate --env integration --enabled AM0001 <path/to/addon_dir>",
	}, "\n")
}

func Cmd() *cobra.Command {
	opts := &options{
		Env: "stage",
	}

	cmd := &cobra.Command{
		Use:     "validate",
		Short:   "Validate addon metadata, bundles and imagesets.",
		Long:    long,
		Example: examples(),
		Args:    cobra.ExactArgs(1),
		RunE:    run(opts),
	}

	flags := cmd.PersistentFlags()

	opts.AddEnvFlag(flags)
	opts.AddVersionFlag(flags)
	opts.AddDisabledFlag(flags)
	opts.AddEnabledFlag(flags)
	opts.AddStagesFlag(flags)

	return cmd
}

var (
	ErrValidationFailed  = errors.New("validation failed")
	ErrValidationErrored = errors.New("validators encountered errors")
)

func run(opts *options) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.VerifyFlags(); err != nil {
			return fmt.Errorf("verifying flags: %w", err)
		}

		addonDir, err := parseAddonDir(args[0])
		if err != nil {
			return fmt.Errorf("parsing addon dir %q: %w", args[0], err)
		}

		if err := verifyAddonDir(addonDir); err != nil {
			return fmt.Errorf("verifying addon dir %q: %w", addonDir, err)
		}

		meta, err := utils.NewMetaLoader(addonDir, opts.Env, opts.Version).Load()
		if err != nil {
			return fmt.Errorf("loading addon metadata from '%s': %w", addonDir, err)
		}

		bundles, err := utils.ExtractAndParseAddons(*meta.IndexImage, meta.OperatorName)
		if err != nil {
			return fmt.Errorf("extracting and parsing addon bundles: %w", err)
		}

		filter, err := generateFilter(opts.Disabled, opts.Enabled, opts.Stages)
		if err != nil {
			return fmt.Errorf("generating validator filter: %w", err)
		}

		runner, err := validator.NewRunner(
			validator.WithMiddleware{
				validator.NewRetryMiddleware(),
			},
		)
		if err != nil {
			return fmt.Errorf("initializing validators: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mb := types.MetaBundle{
			AddonMeta: meta,
			Bundles:   bundles,
		}

		var results validator.ResultList

		for res := range runner.Run(ctx, mb, filter) {
			results = append(results, res)
		}

		sort.Sort(results)

		table, err := cli.NewTable(
			cli.WithHeaders{"STATUS", "CODE", "NAME", "DESCRIPTION", "FAILURE MESSAGE"},
		)
		if err != nil {
			return fmt.Errorf("initializing table: %w", err)
		}
		for _, res := range results {
			writeResult(table, res)
		}

		fmt.Fprintln(os.Stdout, table.String())
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Please consult corresponding validator wikis: https://github.com/mt-sre/addon-metadata-operator/wiki/<code>.")

		if err := runner.CleanUp(); err != nil {
			return fmt.Errorf("cleaning up validators: %w", err)
		}

		if errs := results.Errors(); len(errs) > 0 {
			cli.PrintValidationErrors(errs)
			return ErrValidationErrored
		}

		if results.HasFailure() {
			return ErrValidationFailed
		}

		return nil
	}
}

func parseAddonDir(dir string) (string, error) {
	if !path.IsAbs(dir) {
		return filepath.Abs(dir)
	}

	return dir, nil
}

// addonDir is an absolute path at this point
func verifyAddonDir(addonDir string) error {
	dir, err := os.Stat(addonDir)
	if err != nil {
		return fmt.Errorf("error while reading directory: %w", err)
	}

	if !dir.IsDir() {
		return fmt.Errorf("%q is not a directory", addonDir)
	}

	return nil
}

func generateFilter(disabled, enabled, stages string) (validator.Filter, error) {
	if disabled == "" && enabled == "" && stages == "" {
		return nil, nil
	}

	filter := validator.NoFilter

	if disabled != "" {
		codes, err := parseCodeList(disabled)
		if err != nil {
			return nil, fmt.Errorf("unable to process '--disabled' option argument: %w", err)
		}

		filter = validator.Not(validator.FilterCodes(codes...))
	}

	if enabled != "" {
		codes, err := parseCodeList(enabled)
		if err != nil {
			return nil, fmt.Errorf("unable to process '--enabled' option argument: %w", err)
		}

		filter = validator.FilterCodes(codes...)
	}

	if stages != "" {
		stgs, err := parseStageList(stages)
		if err != nil {
			return nil, fmt.Errorf("unable to process '--stages' option argument: %w", err)
		}

		filter = validator.And(filter, validator.FilterStages(stgs...))
	}

	return filter, nil
}

func parseCodeList(maybeList string) ([]validator.Code, error) {
	rawStrings := strings.Split(maybeList, ",")

	res := make([]validator.Code, 0, len(rawStrings))

	for _, s := range rawStrings {
		c, err := validator.ParseCode(s)
		if err != nil {
			return nil, fmt.Errorf("invalid code list '%s': %w", maybeList, err)
		}

		res = append(res, c)
	}

	return res, nil
}

func parseStageList(maybeList string) ([]validator.Stage, error) {
	rawStrings := strings.Split(maybeList, ",")

	res := make([]validator.Stage, 0, len(rawStrings))

	for _, s := range rawStrings {
		s, err := validator.ParseStage(s)
		if err != nil {
			return nil, fmt.Errorf("invalid stage list %q: %w", maybeList, err)
		}

		res = append(res, s)
	}

	return res, nil
}

func writeResult(t *cli.Table, res validator.Result) {
	row := resultToRow(res)

	if res.IsSuccess() {
		t.WriteRow(append(row, cli.Field{Value: "None"}))
	} else if res.IsError() {
		t.WriteRow(append(row, cli.Field{Value: res.Error.Error()}))
	} else {
		for _, msg := range res.FailureMsgs {
			t.WriteRow(append(row, cli.Field{Value: msg}))
		}
	}
}

func resultToRow(res validator.Result) cli.TableRow {
	var status cli.Field

	if res.IsSuccess() {
		status = cli.Field{
			Value: "Success",
			Color: cli.FieldColorGreen,
		}
	} else if res.IsError() {
		status = cli.Field{
			Value: "Error",
			Color: cli.FieldColorIntenselyBoldRed,
		}
	} else {
		status = cli.Field{
			Value: "Failed",
			Color: cli.FieldColorRed,
		}
	}

	return cli.TableRow{
		status,
		cli.Field{Value: res.Code.String()},
		cli.Field{Value: res.Name},
		cli.Field{Value: res.Description},
	}
}
