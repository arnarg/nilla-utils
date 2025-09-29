{ config }:
let
  inherit (config) lib;
in
{
  options.generators.inputs = {
    pins = lib.options.create {
      type = lib.types.attrs.of lib.types.attrs.any;
      description = "Attribute set with input pins.";
      default.value = { };
    };
  };

  config = {
    inputs = builtins.mapAttrs (n: pin: {
      src = pin;
    }) config.generators.inputs.pins;
  };
}
