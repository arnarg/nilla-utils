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

      # Set the root folder for project-level generators.
      # This will automatically set:
      # - generators.packages.folder = "./packages";
      # - generators.shells.folder = "./shells";
      # - generators.overlays.default.folder = "./packages";
      # - generators.nixos.folder = "./hosts";
      # - generators.home.folder = "./hosts";
      generators.project.folder = ./.;
    };
  }
)
