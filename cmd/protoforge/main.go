// Command protoforge drives the Drizzle -> TypeSpec -> OpenAPI -> .proto pipeline
// from a single protoforge.yaml.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilmoo/protoforge/internal/config"
	"github.com/hilmoo/protoforge/internal/doctor"
	"github.com/hilmoo/protoforge/internal/initcmd"
	"github.com/hilmoo/protoforge/internal/runner"
	"github.com/hilmoo/protoforge/internal/stages"
)

// Set by GoReleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type globals struct {
	config          string
	dryRun          bool
	verbose         bool
	noColor         bool
	continueOnError bool
	specs           []string
}

func main() {
	if err := newRoot().Execute(); err != nil {
		var ee *runner.ExitError
		if errors.As(err, &ee) {
			fmt.Fprintln(os.Stderr, "error:", err)
			if ee.Code > 0 {
				os.Exit(ee.Code)
			}
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "protoforge",
		Short:         "Drizzle → TypeSpec → OpenAPI → .proto, from one config file",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	f := root.PersistentFlags()
	f.StringVarP(&g.config, "config", "c", "", "path to protoforge.yaml (default: search upwards from the current directory)")
	f.BoolVar(&g.dryRun, "dry-run", false, "print commands and file operations, change nothing")
	f.BoolVar(&g.verbose, "verbose", false, "verbose output (also keeps the extracted drizzle script)")
	f.BoolVar(&g.noColor, "no-color", false, "disable colored output")
	f.BoolVar(&g.continueOnError, "continue-on-error", false, "keep going after a failed step and report all failures at the end")
	f.StringSliceVar(&g.specs, "spec", nil, "limit per-spec work to these specs (repeatable or comma-separated; default: every spec)")

	root.AddCommand(initCmd(), runCmd(g), stageCmd(g), doctorCmd(g), configCmd(g), versionCmd())
	return root
}

// setup loads the config and prepares the runner and stage context.
func setup(g *globals) (*stages.Ctx, error) {
	path := g.config
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if path, err = config.Find(wd); err != nil {
			return nil, err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	for _, s := range g.specs {
		if _, ok := cfg.Specs[s]; !ok {
			return nil, fmt.Errorf("unknown spec %q (specs: %s)", s, strings.Join(cfg.SpecNames(), ", "))
		}
	}
	var fileEnv map[string]string
	if cfg.EnvFile != "" {
		p := cfg.Abs("", cfg.EnvFile)
		if _, err := os.Stat(p); err == nil {
			if fileEnv, err = runner.LoadEnvFile(p); err != nil {
				return nil, err
			}
		}
	}
	env := runner.BuildEnv(os.Environ(), fileEnv, cfg.Env)
	r := runner.New(cfg.Root, env)
	r.DryRun, r.Verbose = g.dryRun, g.verbose
	r.Color = !g.noColor && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stderr)
	nr := cfg.Tools.NodeRunner
	if nr == "" {
		nr = runner.DetectNodeRunner(cfg.Root)
	}
	return &stages.Ctx{Cfg: cfg, R: r, NodeRunner: nr, ContinueOnError: g.continueOnError, Specs: g.specs}, nil
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "run [pipeline|stage|stage.step ...]",
		Short:   "Run pipelines, stages and/or single steps in order (default: all)",
		Example: "  protoforge run api\n  protoforge run api --spec billing\n  protoforge run typespec openapi\n  protoforge run typespec.from_drizzle\n  protoforge run migration.docs typespec.compile\n  protoforge run all --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := setup(g)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				args = []string{"all"}
			}
			names, err := ctx.Cfg.Expand(args)
			if err != nil {
				return err
			}
			if ctx.R.DryRun {
				fmt.Fprintf(ctx.R.Stdout, "# dry run: %s -> %s\n", strings.Join(args, " "), strings.Join(names, " "))
			}
			var errs []error
			for _, n := range names {
				stage, step := config.SplitRef(n)
				if !ctx.Cfg.StageEnabled(stage) {
					ctx.R.Warnf(stage, "stage not configured in %s; skipped", config.FileName)
					continue
				}
				s, err := stages.New(ctx, stage)
				if err != nil {
					return err
				}
				var only []string
				if step != "" {
					only = []string{step}
				}
				if err := stages.RunStage(ctx, s, only); err != nil {
					if !ctx.ContinueOnError {
						return err
					}
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		},
	}
}

func stageCmd(g *globals) *cobra.Command {
	var only []string
	c := &cobra.Command{
		Use:   "stage <name>",
		Short: "Run one stage, or selected sub-steps of it with --only",
		Long: `Run one stage. Stages and their steps:

  migration  generate docs push post_push_sql migrate sync run_after
  typespec   from_drizzle compile run_after
  openapi    format go_tags lint run_after
  proto      models services flatten extra_replace cleanup codegen run_after`,
		Example: "  protoforge stage openapi --only format\n  protoforge stage migration --only post_push_sql",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !config.IsStage(args[0]) {
				return fmt.Errorf("unknown stage %q (stages: %s)", args[0], strings.Join(config.StageNames, ", "))
			}
			ctx, err := setup(g)
			if err != nil {
				return err
			}
			s, err := stages.New(ctx, args[0])
			if err != nil {
				return err
			}
			return stages.RunStage(ctx, s, only)
		},
	}
	c.Flags().StringSliceVar(&only, "only", nil, "run only these sub-steps (repeatable or comma-separated)")
	return c
}

func doctorCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the external tools needed by the enabled stages",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := setup(g)
			if err != nil {
				return err
			}
			return doctor.Run(ctx.Cfg, ctx.R, ctx.NodeRunner, os.Stdout)
		},
	}
}

func configCmd(g *globals) *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Inspect the configuration"}
	c.AddCommand(&cobra.Command{
		Use:   "print",
		Short: "Print the resolved config (defaults and env applied)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := setup(g)
			if err != nil {
				return err
			}
			cfg := *ctx.Cfg
			// Show env: values after ${VAR} expansion.
			if len(cfg.Env) > 0 {
				exp := map[string]string{}
				for k := range cfg.Env {
					exp[k] = runner.Getenv(ctx.R.Env, k)
				}
				cfg.Env = exp
			}
			if cfg.Tools.NodeRunner == "" {
				cfg.Tools.NodeRunner = ctx.NodeRunner
			}
			b, err := cfg.Marshal()
			if err != nil {
				return err
			}
			fmt.Printf("# %s\n%s", cfg.Path, b)
			return nil
		},
	})
	var out string
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for protoforge.yaml (for editor validation)",
		Example: "  protoforge config schema -o protoforge.schema.json\n" +
			"  # then add to the top of protoforge.yaml:\n" +
			"  # yaml-language-server: $schema=./protoforge.schema.json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := config.JSONSchema()
			if err != nil {
				return err
			}
			if out == "" {
				_, err = os.Stdout.Write(b)
				return err
			}
			return os.WriteFile(out, b, 0o644)
		},
	}
	schema.Flags().StringVarP(&out, "output", "o", "", "write to a file instead of stdout")
	c.AddCommand(schema)
	return c
}

func initCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Scaffold protoforge.yaml, tspconfig.yaml and oaf-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			return initcmd.Run(initcmd.Options{Dir: wd, Force: force, Out: os.Stdout})
		},
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return c
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the protoforge version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("protoforge %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}
