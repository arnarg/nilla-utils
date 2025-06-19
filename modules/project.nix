{config}: let
  inherit (config) lib;
in {
  options.generators = {
    project.folder = lib.options.create {
      type = lib.types.nullish lib.types.path;
      description = "The folder to auto discover project.";
      default.value = null;
    };
    assertPaths = lib.options.create {
      type = lib.types.bool;
      description = "Whether or not to assert the generator paths exist.";
      internal = true;
      default.value = true;
    };
  };

  config = {
    generators = let
      folder = config.generators.project.folder;
    in
      lib.modules.when
      (folder != null)
      {
        packages.folder = "${folder}/packages";
        shells.folder = "${folder}/shells";
        overlays.default.folder = "${folder}/packages";
        nixos.folder = "${folder}/hosts";
        nixosModules.folder = "${folder}/modules/nixos";
        home.folder = "${folder}/hosts";
        homeModules.folder = "${folder}/modules/home";

        # Since the project generators sets default
        # paths for all we do not want to fail assertions
        # if they don't all exist.
        assertPaths = false;
      };
  };
}
