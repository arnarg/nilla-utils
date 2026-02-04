# NixOS module to add options generateRegistryFromInputs
# and generateNixPathFromInputs.
inputs:
{ config, lib, ... }:
{
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
      builtins.listToAttrs (
        lib.mapAttrsToList (name: input: {
          name = "nix/inputs/${name}";
          value.source = input.src;
        }) inputs
      )
    );

    nix = {
      # Generate registry from inputs
      registry = lib.mkIf (config.nix.generateRegistryFromInputs) (
        lib.mapAttrs (name: input: {
          from = {
            type = "indirect";
            id = name;
          };
          to = {
            type = "path";
            path = input.src;
          };
        }) inputs
      );

      # Add /etc/nix/inputs to NIX_PATH
      nixPath = lib.optionals (config.nix.generateNixPathFromInputs) [ "/etc/nix/inputs" ];
    };
  };
}
