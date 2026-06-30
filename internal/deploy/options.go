package deploy

import (
	"fmt"

	"charm.land/log/v2"
	"github.com/arnarg/nilla-utils/internal/nix"
	"github.com/arnarg/nilla-utils/internal/project"
	"github.com/arnarg/nilla-utils/internal/util"
)

type Command int

const (
	Build Command = iota
	Test
	Boot
	Switch
)

func (c Command) String() string {
	switch c {
	case Build:
		return "Build"
	case Test:
		return "Test"
	case Boot:
		return "Boot"
	case Switch:
		return "Switch"
	default:
		return "Unknown"
	}
}

type Options struct {
	ProjectPath string
	Name        string
	SubCmd      Command

	BuildOn     string
	BuildOnSelf bool
	Target      string

	Raw     bool
	Verbose bool
	Compact bool

	NoLink  bool
	OutLink string

	Confirm bool
	Notify  bool
}

type Plan struct {
	Source *project.ProjectSource
	Attr   string
	Name   string
	SubCmd Command

	BuildTarget  string
	DeployTarget string
	StoreAddr    string

	Raw     bool
	Verbose bool
	Compact bool

	NoLink  bool
	OutLink string

	Confirm bool
	Notify  bool
}

func ResolvePlan(opts Options, sys System) (*Plan, error) {
	// Resolve project
	source, err := project.Resolve(opts.ProjectPath)
	if err != nil {
		return nil, err
	}

	// Resolve system name for either NixOS or home-manager
	name, err := sys.ResolveName(opts.Name, source.FullNillaPath())
	if err != nil {
		return nil, err
	}

	// Get the toplevel attribute for either NixOS or home-manager
	attr := sys.AttrPath(name)

	log.Infof("Found system \"%s\"", name)

	// Check if the toplevel attribute exists in the nilla project
	exists, err := nix.ExistsInProject(source.NillaPath, source.FixedOutputStoreEntry(), attr)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Attribute '%s' does not exist in project \"%s\"", attr, source.FullNillaPath())
	}

	// Infer build target
	buildTarget := ""
	if opts.BuildOn != "" {
		buildTarget = opts.BuildOn
	} else if opts.BuildOnSelf {
		if opts.Target == "" {
			return nil, fmt.Errorf("--build-on-target requires --target to be specified")
		}
		buildTarget = opts.Target
	}

	// Find store address for remote build (if enabled)
	storeAddr := ""
	if buildTarget != "" {
		user, hostname := util.ParseTarget(buildTarget)
		storeAddr = util.BuildStoreAddress(user, hostname)
	}

	return &Plan{
		Source:       source,
		Attr:         attr,
		Name:         name,
		SubCmd:       opts.SubCmd,
		BuildTarget:  buildTarget,
		DeployTarget: opts.Target,
		StoreAddr:    storeAddr,
		Raw:          opts.Raw,
		Verbose:      opts.Verbose,
		Compact:      opts.Compact,
		NoLink:       opts.NoLink,
		OutLink:      opts.OutLink,
		Confirm:      opts.Confirm,
		Notify:       opts.Notify,
	}, nil
}
