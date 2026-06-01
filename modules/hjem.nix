{ config }:
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
        description = "Whether to recursively search for modules.";
      };
    };
  };

  config = {
    # Generate hjem configurations from `generators.hjem`
    modules.hjem =
      let
        loader =
          if config.generators.hjemModules.recursive then
            lib.utils.loadDirsWithFileRecursive
          else
            lib.utils.loadDirsWithFile;
      in
      lib.modules.when (
        config.generators.hjemModules.folder != null && pathExists config.generators.hjemModules.folder
      ) (mapAttrs (_name: import) (loader "default.nix" config.generators.hjemModules.folder));
  };
}
