{ config }:
let
  inherit (config) inputs lib;
  inherit (builtins) pathExists;

  globalModules = config.modules;

  nixosModule = import ./nixos-module.nix inputs;
in
{
  includes = [
    ./lib.nix
  ];

  options = {
    systems.microvm = lib.options.create {
      description = "MicroVM systems to create.";
      default.value = { };
      type = lib.types.attrs.of (
        lib.types.submodule (
          { name, config }:
          {
            options = {
              args = lib.options.create {
                description = "Additional arguments to pass to system modules.";
                type = lib.types.attrs.any;
                default.value = { };
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
                default.value = if inputs ? nixpkgs then inputs.nixpkgs else null;
              };

              microvm = lib.options.create {
                description = "The microvm input to use.";
                type = lib.types.raw;
                default.value = if inputs ? microvm then inputs.microvm else null;
              };

              modules = lib.options.create {
                description = "A list of modules to use for the system.";
                type = lib.types.list.of lib.types.raw;
                default.value = [ ];
              };

              result = lib.options.create {
                description = "The created MicroVM system.";
                type = lib.types.raw;
                writable = false;
                default.value = import "${config.nixpkgs.src}/nixos/lib/eval-config.nix" {
                  # This needs to be set to null in order for pure evaluation to work
                  system = null;
                  lib = import "${config.nixpkgs.src}/lib";
                  specialArgs = {
                    nixosModules = if globalModules ? "nixos" then globalModules.nixos else { };
                  }
                  // config.args;
                  modules = config.modules ++ [
                    (
                      { lib, ... }:
                      {
                        # Set settings from nixpkgs input as defaults.
                        nixpkgs = {
                          overlays = config.nixpkgs.settings.overlays or [ ];

                          # Set every leaf in inputs.nixpkgs.settings.configuration
                          # as default with `mkDefault` so it can be overwritten
                          # more easily in a module.
                          config = lib.mapAttrsRecursive (_: lib.mkDefault) (config.nixpkgs.settings.configuration or { });

                          # Higher priority than `mkOptionDefault` but lower than `mkDefault`.
                          hostPlatform = lib.mkOverride 1400 config.system;
                        };

                        # Pass MicroVM name to microvm module.
                        nutils.vmName = name;
                      }
                    )
                    nixosModule
                    "${config.microvm.src}/nixos-modules/microvm"
                    ./microvm-module.nix
                  ];
                  modulesLocation = null;
                };
              };
            };
          }
        )
      );
    };
  };

  config = {
    assertions =
      lib.attrs.mapToList (name: value: {
        assertion = !(builtins.isNull value.nixpkgs);
        message = "A Nixpkgs instance is required for the MicroVM system \"${name}\", but none was provided and \"inputs.nixpkgs\" does not exist.";
      }) config.systems.microvm
      ++ lib.attrs.mapToList (name: value: {
        assertion = !(builtins.isNull value.microvm);
        message = "A microvm instance is required for the MicroVM system \"${name}\", but none was provided and \"inputs.microvm\" does not exist.";
      }) config.systems.microvm;
  };
}
