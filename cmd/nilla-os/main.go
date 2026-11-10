package main

import (
	"context"
	"fmt"
	"os"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/deploy"
	"github.com/arnarg/nilla-utils/internal/gencmd"
	"github.com/arnarg/nilla-utils/internal/generation"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/urfave/cli/v3"
)

var version = "unknown"

var description = `[name]  Name of the NixOS system to build. If left empty it will use current hostname.`

var verboseCount int

func actionFuncFor(sc deploy.Command) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		return run(ctx, cmd, sc)
	}
}

var app = &cli.Command{
	Name:                   "nilla-os",
	Version:                version,
	Usage:                  "A nilla cli plugin to work with NixOS configurations.",
	HideVersion:            true,
	HideHelpCommand:        true,
	UseShortOptionHandling: true,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:        "version",
			Aliases:     []string{"V"},
			Usage:       "Print version",
			HideDefault: true,
			Local:       true,
			Action: func(ctx context.Context, cmd *cli.Command, _ bool) error {
				cli.ShowVersion(cmd)
				return cli.Exit("", 0)
			},
		},
		&cli.BoolFlag{
			Name:        "verbose",
			Aliases:     []string{"v"},
			Usage:       "Set log level to verbose (pass multiple times, e.g. -vv for SSH debug)",
			HideDefault: true,
			Config: cli.BoolConfig{
				Count: &verboseCount,
			},
		},
		&cli.BoolFlag{
			Name:    "compact",
			Usage:   "Make build and copy progress view more compact",
			Sources: cli.EnvVars("NILLA_UTILS_TUI_COMPACT"),
		},
		&cli.BoolFlag{
			Name:  "raw",
			Usage: "Raw output from Nix",
		},
		&cli.BoolFlag{
			Name:    "notify",
			Usage:   "Send a desktop notification when a build is ready for confirmation",
			Sources: cli.EnvVars("NILLA_UTILS_NOTIFY_CONFIRM"),
		},
		&cli.StringFlag{
			Name:    "project",
			Aliases: []string{"p"},
			Usage:   "The nilla project to use",
			Value:   "./",
		},
		&cli.StringFlag{
			Name:    "target",
			Aliases: []string{"t"},
			Usage:   "Target host to deploy/activate on (for switch/test/boot commands). Can also be used with --build-on-target for builds.",
		},
		&cli.StringFlag{
			Name:  "build-on",
			Usage: "Build on the specified host instead of locally. Dependencies are fetched from target's substituters.",
		},
		&cli.BoolFlag{
			Name:  "build-on-target",
			Usage: "Build on the same host as specified by --target (requires --target flag). Dependencies are fetched from target's substituters.",
		},
	},
	Commands: []*cli.Command{
		// Build
		{
			Name:        "build",
			Usage:       "Build NixOS configuration",
			Description: fmt.Sprintf("Build NixOS configuration.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "no-link",
					Usage: "Do not create symlinks to the build results",
				},
				&cli.BoolFlag{
					Name:  "print-out-paths",
					Usage: "Print the resulting output paths",
				},
				&cli.StringFlag{
					Name:    "out-link",
					Aliases: []string{"o"},
					Usage:   "Use path as prefix for the symlinks to the build results",
				},
			},
			Action: actionFuncFor(deploy.Build),
		},

		// Test
		{
			Name:        "test",
			Usage:       "Build NixOS configuration and activate it",
			Description: fmt.Sprintf("Build NixOS configuration and activate it.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
				&cli.BoolFlag{
					Name:  "no-diff",
					Usage: "Skip comparing generations before activating",
				},
			},
			Action: actionFuncFor(deploy.Test),
		},

		// Boot
		{
			Name:        "boot",
			Usage:       "Build NixOS configuration and make it the boot default",
			Description: fmt.Sprintf("Build NixOS configuration and make it the boot default.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
				&cli.BoolFlag{
					Name:  "no-diff",
					Usage: "Skip comparing generations before activating",
				},
			},
			Action: actionFuncFor(deploy.Boot),
		},

		// Switch
		{
			Name:        "switch",
			Usage:       "Build NixOS configuration, activate it and make it the boot default",
			Description: fmt.Sprintf("Build NixOS configuration, activate it and make it the boot default.\n\n%s", description),
			ArgsUsage:   "[name]",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation",
				},
				&cli.BoolFlag{
					Name:  "no-diff",
					Usage: "Skip comparing generations before activating",
				},
			},
			Action: actionFuncFor(deploy.Switch),
		},

		// Install
		{
			Name:  "install",
			Usage: "Install NixOS configuration onto a host booted into a NixOS installer",
			Description: `Install NixOS configuration onto a host booted into a NixOS installer.

The system is built locally, its closure copied straight into the target's
root mount point and installed with nixos-install. Requires --target, and
the target must be logged into as root.

With --build-on the system is built on that host and copied from there. With
--build-on-target it is built on the target, straight into the root mount
point, using the binary caches from the system's nix.settings. Unless --disk
is none, that happens after the disks are prepared, so the confirmation comes
before the system is built.

No root password is set during install, so the configuration must set one
or add SSH keys for a user to be able to log in afterwards.

If the system uses disko, choose how disks are prepared with --disk:
  none     Disks are already partitioned and mounted at the root mount point
  mount    Mount existing disko filesystems
  format   Create missing disko partitions and filesystems, then mount them
           (existing filesystems are kept)
  destroy  Wipe all disko disks, recreate and mount them

<name>  Name of the NixOS system to install.`,
			ArgsUsage: "<name>",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "disk",
					Usage: "How to prepare disks with disko: none, mount, format or destroy",
				},
				&cli.StringFlag{
					Name:  "root",
					Usage: "Mount point on the target to install into (defaults to disko.rootMountPoint when using disko)",
					Value: "/mnt",
				},
				&cli.StringFlag{
					Name:  "extra-files",
					Usage: "Directory whose contents are copied into the root mount point before installing, e.g. etc/ssh/ssh_host_ed25519_key",
				},
				&cli.BoolFlag{
					Name:    "substitute-on-destination",
					Aliases: []string{"s"},
					Usage:   "Let the target fetch store paths from its substituters instead of copying them",
				},
				&cli.BoolFlag{
					Name:  "no-reboot",
					Usage: "Leave the target mounted and running the installer when done",
				},
				&cli.BoolFlag{
					Name:  "no-installer-check",
					Usage: "Skip checking that the target is booted into a NixOS installer",
				},
				&cli.BoolFlag{
					Name:    "confirm",
					Aliases: []string{"c"},
					Usage:   "Do not ask for confirmation, including before wiping disks",
				},
			},
			Action: install,
		},

		// List
		{
			Name:        "list",
			Aliases:     []string{"ls"},
			Usage:       "List NixOS configurations in project",
			Description: "List NixOS configurations in project",
			Action:      listConfigurations,
		},

		// Generations
		gencmd.Command(generation.NixOSSystem{}, "NixOS", &verboseCount),
	},
}

func printSection(text string) {
	fmt.Fprintf(os.Stderr, "\033[32m>\033[0m %s\n", text)
}

func run(ctx context.Context, cmd *cli.Command, sc deploy.Command) error {
	util.InitLogger(verboseCount)

	plan, err := deploy.ResolvePlan(deploy.Options{
		ProjectPath:   cmd.String("project"),
		Name:          cmd.Args().First(),
		SubCmd:        sc,
		BuildOn:       cmd.String("build-on"),
		BuildOnSelf:   cmd.Bool("build-on-target"),
		Target:        cmd.String("target"),
		Raw:           cmd.Bool("raw"),
		Verbose:       cmd.Bool("verbose"),
		Compact:       cmd.Bool("compact"),
		NoLink:        cmd.Bool("no-link"),
		OutLink:       cmd.String("out-link"),
		PrintOutPaths: cmd.Bool("print-out-paths"),
		Confirm:       cmd.Bool("confirm"),
		Notify:        cmd.Bool("notify"),
		NoDiff:        cmd.Bool("no-diff"),
	}, deploy.NixOSSystem{})
	if err != nil {
		return err
	}

	s, err := deploy.NewSession(ctx, plan, deploy.NixOSSystem{}, deploy.DefaultDeps())
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Run(ctx)
}

func install(ctx context.Context, cmd *cli.Command) error {
	util.InitLogger(verboseCount)

	name := cmd.Args().First()
	if name == "" {
		return fmt.Errorf("install requires the name of the system to install")
	}
	if cmd.String("target") == "" {
		return fmt.Errorf("install requires --target")
	}

	disk, err := deploy.ParseDiskMode(cmd.String("disk"))
	if err != nil {
		return err
	}

	plan, err := deploy.ResolvePlan(deploy.Options{
		ProjectPath: cmd.String("project"),
		Name:        name,
		SubCmd:      deploy.Install,
		Target:      cmd.String("target"),
		BuildOn:     cmd.String("build-on"),
		BuildOnSelf: cmd.Bool("build-on-target"),
		Raw:         cmd.Bool("raw"),
		Verbose:     cmd.Bool("verbose"),
		Compact:     cmd.Bool("compact"),
		Confirm:     cmd.Bool("confirm"),
		Notify:      cmd.Bool("notify"),
		NoDiff:      true,
	}, deploy.NixOSSystem{})
	if err != nil {
		return err
	}

	s, err := deploy.NewSession(ctx, plan, deploy.NixOSSystem{}, deploy.DefaultDeps())
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Install(ctx, deploy.InstallOptions{
		Disk:             disk,
		Root:             cmd.String("root"),
		RootSet:          cmd.IsSet("root"),
		ExtraFiles:       cmd.String("extra-files"),
		Substitute:       cmd.Bool("substitute-on-destination"),
		NoReboot:         cmd.Bool("no-reboot"),
		NoInstallerCheck: cmd.Bool("no-installer-check"),
	})
}

func listConfigurations(ctx context.Context, cmd *cli.Command) error {
	// Setup logger
	util.InitLogger(verboseCount)

	// Resolve project
	source, err := project.Resolve(cmd.String("project"))
	if err != nil {
		return err
	}

	// Get a list of home systems
	systems, err := nix.ListAttrsInProject(source.NillaPath, source.FixedOutputStoreEntry(), "systems.nixos")
	if err != nil {
		return err
	}

	// Print results
	if len(systems) < 1 {
		fmt.Println("No NixOS configurations found")
	} else {
		printSection("NixOS configurations")
		for _, system := range systems {
			fmt.Printf("- %s\n", system)
		}
	}

	return nil
}

func main() {
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}
