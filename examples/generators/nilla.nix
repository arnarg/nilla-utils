let
  pins = import ../../npins;

  nilla = import pins.nilla;
in
nilla.create (
  { config }:
  {
    includes = [
      ../../modules
    ];

    config = {
      # Use inputs generator to load all
      # inputs from npins.
      generators.inputs.pins = pins;

      # Use packages generator to load all
      # package definitions in sub-directories
      # of ./packages
      generators.packages.folder = ./packages;

      # Use shells generator to load all
      # shell definitions in sub-directories
      # of ./shells
      generators.shells.folder = ./shells;
    };
  }
)
