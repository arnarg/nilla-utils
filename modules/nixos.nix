{config}: let
  inherit (config) inputs lib;
  inherit (builtins) listToAttrs mapAttrs pathExists;

  globalModules = config.modules;

  # NixOS module to add options generateRegistryFromInputs
  # and generateNixPathFromInputs.
  nixosModule = {
    config,
    lib,
    ...
  }: {
    options.nix = {
      generateRegistryFromInputs = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Automatically add all inputs to nix registry.";
      };
      generateNixPathFromInputs = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = "Automatically add all inputs to $NIX_PATH.";
      };
    };

    config = {
      # Add all inputs in /etc/nix/inputs
      environment.etc = lib.mkIf (config.nix.generateNixPathFromInputs) (
        listToAttrs (lib.mapAttrsToList (name: input: {
            name = "nix/inputs/${name}";
            value.source = input.src;
          })
          inputs)
      );

      nix = {
        # Generate registry from inputs
        registry = lib.mkIf (config.nix.generateRegistryFromInputs) (lib.mapAttrs (name: input: {
            from = {
              type = "indirect";
              id = name;
            };
            to = {
              type = "path";
              path = input.src;
            };
          })
          inputs);

        # Add /etc/nix/inputs to NIX_PATH
        nixPath = lib.optionals (config.nix.generateNixPathFromInputs) ["/etc/nix/inputs"];
      };
    };
  };
in {
  includes = [
    ./lib.nix
  ];

  options = {
    systems.nixos = lib.options.create {
      description = "NixOS systems to create.";
      default.value = {};
      type = lib.types.attrs.of (lib.types.submodule ({config}: {
        options = {
          args = lib.options.create {
            description = "Additional arguments to pass to system modules.";
            type = lib.types.attrs.any;
            default.value = {};
          };

          system = lib.options.create {
            description = ''
              The hostPlatform of the host. The NixOS option `nixpkgs.hostPlatform` in a NixOS module takes precedence over this.
            '';
            type = lib.types.string;
            default.value = "x86_64-linux";
          };

          nixpkgs = lib.options.create {
            description = "The Nixpkgs input to use.";
            type = lib.types.raw;
            default.value =
              if inputs ? nixpkgs
              then inputs.nixpkgs
              else null;
          };

          modules = lib.options.create {
            description = "A list of modules to use for the system.";
            type = lib.types.list.of lib.types.raw;
            default.value = [];
          };

          result = lib.options.create {
            description = "The created NixOS system.";
            type = lib.types.raw;
            writable = false;
            default.value = import "${config.nixpkgs.src}/nixos/lib/eval-config.nix" {
              # This needs to be set to null in order for pure evaluation to work
              system = null;
              lib = import "${config.nixpkgs.src}/lib";
              specialArgs =
                {
                  nixosModules =
                    if globalModules ? "nixos"
                    then globalModules.nixos
                    else {};
                }
                // config.args;
              modules =
                config.modules
                ++ [
                  (
                    {lib, ...}: {
                      # Set settings from nixpkgs input as defaults.
                      nixpkgs = {
                        overlays = config.nixpkgs.settings.overlays or [];

                        # Set every leaf in inputs.nixpkgs.settings.configuration
                        # as default with `mkDefault` so it can be overwritten
                        # more easily in a module.
                        config = lib.mapAttrsRecursive (_: lib.mkDefault) (config.nixpkgs.settings.configuration or {});

                        # Higher priority than `mkOptionDefault` but lower than `mkDefault`.
                        hostPlatform = lib.mkOverride 1400 config.system;
                      };
                    }
                  )
                  nixosModule
                ];
              modulesLocation = null;
            };
          };
        };
      }));
    };

    generators.nixos = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover NixOS hosts.";
        default.value = null;
      };
      args = lib.options.create {
        description = "Additional arguments to pass to system modules.";
        type = lib.types.attrs.any;
        default.value = {};
      };
      modules = lib.options.create {
        type = lib.types.list.of lib.types.raw;
        default.value = [];
        description = "Default modules to include in all hosts.";
      };
    };

    generators.nixosModules = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover NixOS modules.";
        default.value = null;
      };
    };
  };

  config = {
    assertions =
      (lib.lists.when config.generators.assertPaths [
        {
          assertion =
            config.generators.nixos.folder
            == null
            || (config.generators.nixos.folder != null && pathExists config.generators.nixos.folder);
          message = "NixOS generator's folder \"${config.generators.nixos.folder}\" does not exist.";
        }
        {
          assertion =
            config.generators.nixosModules.folder
            == null
            || (config.generators.nixosModules.folder != null && pathExists config.generators.nixosModules.folder);
          message = "NixOS modules generator's folder \"${config.generators.nixosModules.folder}\" does not exist.";
        }
      ])
      ++ (lib.attrs.mapToList
        (name: value: {
          assertion = !(builtins.isNull value.nixpkgs);
          message = "A Nixpkgs instance is required for the NixOS system \"${name}\", but none was provided and \"inputs.nixpkgs\" does not exist.";
        })
        config.systems.nixos);

    # Generate NixOS configurations from `generators.nixos`
    systems.nixos =
      lib.modules.when
      (config.generators.nixos.folder != null && pathExists config.generators.nixos.folder)
      (listToAttrs (map (host: {
        name = host.hostname;
        value = {
          args =
            {
              inputs = config.inputs;
            }
            // config.generators.nixos.args;
          modules =
            [
              host.configuration
              ({lib, ...}: {
                # Automatically set hostname
                networking.hostName = lib.mkDefault host.hostname;
              })
            ]
            ++ config.generators.nixos.modules;
        };
      }) (lib.utils.loadHostsFromDir config.generators.nixos.folder "configuration.nix")));

    # Generate NixOS modules from `generators.nixosModules`
    modules.nixos =
      lib.modules.when
      (config.generators.nixosModules.folder != null && pathExists config.generators.nixosModules.folder)
      (
        mapAttrs
        (_name: import)
        (
          lib.utils.loadDirsWithFile
          "default.nix"
          config.generators.nixosModules.folder
        )
      );
  };
}
