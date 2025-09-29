{ config }:
let
  inherit (config) lib;
  inherit (builtins) mapAttrs pathExists;
in
{
  options.generators.packages = {
    folder = lib.options.create {
      type = lib.types.nullish lib.types.path;
      description = "The folder to auto discover packages.";
      default.value = null;
    };
    builder = lib.options.create {
      type = lib.types.string;
      description = "The builder to use for the generated packages.";
      default.value = "nixpkgs";
    };
    settings =
      let
        builder = config.builders.${config.generators.packages.builder};
      in
      lib.options.create {
        description = "Additional configuration to use when loading when loading the packages.";
        type = builder.settings.type;
        default.value = builder.settings.default;
      };
    systems = lib.options.create {
      description = "The systems to build the packages for.";
      type = lib.types.list.of lib.types.string;
      default.value = [
        "x86_64-linux"
        "aarch64-linux"
      ];
    };
  };

  config = {
    assertions =
      let
        folder = config.generators.packages.folder;
      in
      lib.lists.when config.generators.assertPaths [
        {
          assertion = folder == null || (folder != null && pathExists folder);
          message = "Packages generator's configured folder \"${toString folder}\" does not exist.";
        }
      ];

    packages =
      lib.modules.when
        (config.generators.packages.folder != null && pathExists config.generators.packages.folder)
        (
          mapAttrs (name: dir: {
            inherit (config.generators.packages) systems builder settings;
            package = import dir;
          }) (lib.utils.loadDirsWithFile "default.nix" config.generators.packages.folder)
        );
  };
}
