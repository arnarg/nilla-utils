{ config }:
let
  inherit (config) inputs lib;
  inherit (builtins) listToAttrs mapAttrs pathExists;

  globalModules = config.modules;

  # The registry and NIX_PATH options only use `nix.registry`, `nix.nixPath`
  # and `environment.etc`, which nix-darwin provides as well.
  darwinModule = import ./nixos-module.nix inputs;
in
{
  includes = [
    ./lib.nix
  ];

  options = {
    systems.darwin = lib.options.create {
      description = "nix-darwin systems to create.";
      default.value = { };
      type = lib.types.attrs.of (
        lib.types.submodule (
          { config }:
          {
            options = {
              args = lib.options.create {
                description = "Additional arguments to pass to system modules.";
                type = lib.types.attrs.any;
                default.value = { };
              };

              system = lib.options.create {
                description = ''
                  The hostPlatform of the host. The nix-darwin option `nixpkgs.hostPlatform` in a module takes precedence over this.
                '';
                type = lib.types.string;
                default.value = "aarch64-darwin";
              };

              nixpkgs = lib.options.create {
                description = "The Nixpkgs input to use.";
                type = lib.types.raw;
                default.value = if inputs ? nixpkgs then inputs.nixpkgs else null;
              };

              nix-darwin = lib.options.create {
                description = "The nix-darwin input to use.";
                type = lib.types.raw;
                default.value = if inputs ? nix-darwin then inputs.nix-darwin else null;
              };

              modules = lib.options.create {
                description = "A list of modules to use for the system.";
                type = lib.types.list.of lib.types.raw;
                default.value = [ ];
              };

              result = lib.options.create {
                description = "The created nix-darwin system.";
                type = lib.types.raw;
                writable = false;
                default.value = import "${config.nix-darwin.src}/eval-config.nix" {
                  lib = import "${config.nixpkgs.src}/lib";
                  specialArgs = {
                    darwinModules = if globalModules ? "darwin" then globalModules.darwin else { };
                  }
                  // config.args;
                  modules = config.modules ++ [
                    (
                      { lib, ... }:
                      {
                        nixpkgs = {
                          # nix-darwin imports nixpkgs from this path. It
                          # defaults to `<nixpkgs>`, which is unavailable in
                          # pure evaluation.
                          source = lib.mkDefault config.nixpkgs.src;

                          overlays = config.nixpkgs.settings.overlays or [ ];

                          # Set every leaf in inputs.nixpkgs.settings.configuration
                          # as default with `mkDefault` so it can be overwritten
                          # more easily in a module.
                          config = lib.mapAttrsRecursive (_: lib.mkDefault) (config.nixpkgs.settings.configuration or { });

                          # Higher priority than `mkOptionDefault` but lower than `mkDefault`.
                          hostPlatform = lib.mkOverride 1400 config.system;
                        };

                        # This check expects a `darwin-config` entry in NIX_PATH,
                        # which only applies to channel based setups.
                        system.checks.verifyNixPath = lib.mkDefault false;
                      }
                    )
                    darwinModule
                  ];
                };
              };
            };
          }
        )
      );
    };

    generators.darwin = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover nix-darwin hosts.";
        default.value = null;
      };
      args = lib.options.create {
        description = "Additional arguments to pass to system modules.";
        type = lib.types.attrs.any;
        default.value = { };
      };
      modules = lib.options.create {
        type = lib.types.list.of lib.types.raw;
        default.value = [ ];
        description = "Default modules to include in all hosts.";
      };
    };

    generators.darwinModules = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover nix-darwin modules.";
        default.value = null;
      };
      recursive = lib.options.create {
        type = lib.types.bool;
        default.value = false;
        description = "Whether to recursively search for modules.";
      };
    };
  };

  config = {
    assertions =
      (lib.lists.when config.generators.assertPaths [
        {
          assertion =
            config.generators.darwin.folder == null
            || (config.generators.darwin.folder != null && pathExists config.generators.darwin.folder);
          message = "nix-darwin generator's folder \"${config.generators.darwin.folder}\" does not exist.";
        }
        {
          assertion =
            config.generators.darwinModules.folder == null
            || (
              config.generators.darwinModules.folder != null
              && pathExists config.generators.darwinModules.folder
            );
          message = "nix-darwin modules generator's folder \"${config.generators.darwinModules.folder}\" does not exist.";
        }
      ])
      ++ (lib.attrs.mapToList (name: value: {
        assertion = !(builtins.isNull value.nixpkgs);
        message = "A Nixpkgs instance is required for the nix-darwin system \"${name}\", but none was provided and \"inputs.nixpkgs\" does not exist.";
      }) config.systems.darwin)
      ++ (lib.attrs.mapToList (name: value: {
        assertion = !(builtins.isNull value.nix-darwin);
        message = "A nix-darwin input is required for the nix-darwin system \"${name}\", but none was provided and \"inputs.nix-darwin\" does not exist.";
      }) config.systems.darwin);

    # Generate nix-darwin configurations from `generators.darwin`
    systems.darwin =
      lib.modules.when
        (config.generators.darwin.folder != null && pathExists config.generators.darwin.folder)
        (
          listToAttrs (
            map (host: {
              name = host.hostname;
              value = {
                args = {
                  inputs = config.inputs;
                  host = host.hostname;
                }
                // config.generators.darwin.args;
                modules = [
                  host.configuration
                  (
                    { lib, ... }:
                    {
                      # Automatically set hostname
                      networking.hostName = lib.mkDefault host.hostname;
                    }
                  )
                ]
                ++ config.generators.darwin.modules;
              };
            }) (lib.utils.loadHostsFromDir config.generators.darwin.folder "configuration.nix")
          )
        );

    # Generate nix-darwin modules from `generators.darwinModules`
    modules.darwin =
      let
        loader =
          if config.generators.darwinModules.recursive then
            lib.utils.loadDirsWithFileRecursive
          else
            lib.utils.loadDirsWithFile;
      in
      lib.modules.when (
        config.generators.darwinModules.folder != null && pathExists config.generators.darwinModules.folder
      ) (mapAttrs (_name: import) (loader "default.nix" config.generators.darwinModules.folder));
  };
}
