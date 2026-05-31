{
  config,
}:

let
  inherit (config) lib;
  inherit (builtins)
    mapAttrs
    pathExists
    ;
in

{
  includes = [
    ./lib.nix
  ];

  options = {
    generators.hjemModules = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover hjem modules.";
        default.value = null;
      };

      recursive = lib.options.create {
        type = lib.types.bool;
        default.value = false;
        description = "Either for recursive search modules.";
      };
    };
  };

  config = {
    # Generate hjem configurations from `generators.hjem`
    modules.hjem =
      lib.modules.when
        (config.generators.hjemModules.folder != null && pathExists config.generators.hjemModules.folder)
        (
          mapAttrs (_name: import) (
            (
              if config.generators.hjemModules.recursive then
                lib.utils.loadDirsWithFileRecursive
              else
                lib.utils.loadDirsWithFile
            )
              "default.nix"
              config.generators.hjemModules.folder
          )
        );
  };
}
