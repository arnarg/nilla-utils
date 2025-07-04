{config}: let
  inherit (config) lib;
  inherit (builtins) mapAttrs pathExists;
in {
  options.generators.shells = {
    folder = lib.options.create {
      type = lib.types.nullish lib.types.path;
      description = "The folder to auto discover shells.";
      default.value = null;
    };
    builder = lib.options.create {
      type = lib.types.string;
      description = "The builder to use for the generated shells.";
      default.value = "nixpkgs";
    };
    settings = let
      builder = config.builders.${config.generators.shells.builder};
    in
      lib.options.create {
        description = "Additional configuration to use when loading when loading the shells.";
        type = builder.settings.type;
        default.value = builder.settings.default;
      };
    systems = lib.options.create {
      description = "The systems to build the shells for.";
      type = lib.types.list.of lib.types.string;
      default.value = ["x86_64-linux" "aarch64-linux"];
    };
  };

  config = {
    assertions = let
      folder = config.generators.shells.folder;
    in
      lib.lists.when config.generators.assertPaths [
        {
          assertion = folder == null || (folder != null && pathExists folder);
          message = "Shells generator's configured folder \"${toString folder}\" does not exist.";
        }
      ];

    shells =
      lib.modules.when
      (config.generators.shells.folder != null && pathExists config.generators.shells.folder)
      (
        mapAttrs
        (name: dir: {
          inherit (config.generators.shells) systems builder settings;
          shell = import dir;
        })
        (
          lib.utils.loadDirsWithFile
          "default.nix"
          config.generators.shells.folder
        )
      );
  };
}
