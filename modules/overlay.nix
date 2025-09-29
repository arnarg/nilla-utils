{ config }:
let
  inherit (config) lib;
  inherit (builtins) mapAttrs pathExists;
in
{
  options = {
    overlays = lib.options.create {
      description = "Overlays which are available from this Nilla project.";
      type = lib.types.attrs.of lib.types.raw;
      default.value = { };
    };

    generators.overlays = lib.options.create {
      description = "Overlays that should be generated from a directory structure.";
      default.value = { };
      type = lib.types.attrs.lazy (
        lib.types.submodule (
          { config }:
          {
            options.folder = lib.options.create {
              type = lib.types.path;
              description = "The folder to auto discover packages.";
            };
            options.args = lib.options.create {
              description = "Additional arguments to pass to overlayed packages.";
              type = lib.types.attrs.any;
              default.value = { };
            };
          }
        )
      );
    };
  };

  config = {
    assertions = lib.lists.when config.generators.assertPaths (
      lib.attrs.mapToList (name: opts: {
        assertion = pathExists opts.folder;
        message = "Overlay generator's folder \"${toString opts.folder}\" for overlay \"${name}\" does not exist.";
      }) config.generators.overlays
    );

    overlays =
      let
        mkOverlayFromDir =
          dir: args:
          (f: p: (mapAttrs (_: d: f.callPackage d args) (lib.utils.loadDirsWithFile "default.nix" dir)));
      in
      mapAttrs (
        _: opts: lib.modules.when (pathExists opts.folder) (mkOverlayFromDir opts.folder opts.args)
      ) config.generators.overlays;
  };
}
