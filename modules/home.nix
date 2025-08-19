{config}: let
  inherit (config) inputs lib;
  inherit (builtins) listToAttrs mapAttrs pathExists concatMap;

  globalModules = config.modules;
in {
  includes = [
    ./lib.nix
  ];

  options = {
    systems.home = lib.options.create {
      description = "home-manager systems to create.";
      default.value = {};
      type = lib.types.attrs.of (lib.types.submodule ({config}: {
        options = {
          args = lib.options.create {
            description = "Additional arguments to pass to home-manager modules.";
            type = lib.types.attrs.any;
            default.value = {};
          };

          system = lib.options.create {
            description = "The system of pkgs to use.";
            type = lib.types.string;
            default.value = "x86_64-linux";
          };

          home-manager = lib.options.create {
            description = "The home-manager input to use.";
            type = lib.types.raw;
            default.value =
              if inputs ? home-manager
              then inputs.home-manager
              else null;
          };

          pkgs = lib.options.create {
            description = "The Nixpkgs instance to use.";
            type = lib.types.raw;
            default.value =
              if
                inputs
                ? nixpkgs
                && inputs.nixpkgs.result ? ${config.system}
              then inputs.nixpkgs.result.${config.system}
              else null;
          };

          modules = lib.options.create {
            description = "A list of modules to use for home-manager.";
            type = lib.types.list.of lib.types.raw;
            default.value = [];
          };

          result = lib.options.create {
            description = "The created home-manager system.";
            type = lib.types.raw;
            writable = false;
            default.value = let
              src = config.home-manager.src;
              contents = builtins.readDir src;
              directories = lib.attrs.filter (name: value: value == "directory") contents;

              builder =
                if directories ? "lib" && (builtins.readDir "${src}/lib") ? "default.nix"
                then (import "${src}/lib" {inherit (config.pkgs) lib;}).homeManagerConfiguration
                else
                  {
                    pkgs,
                    lib,
                    extraSpecialArgs,
                    modules,
                  }:
                    import "${src}/modules" {
                      inherit pkgs lib extraSpecialArgs;
                      check = true;
                      configuration = {lib, ...}: {
                        imports = modules;
                        nixpkgs = {
                          config = lib.mkDefault pkgs.config;
                          inherit (pkgs) overlays;
                        };
                      };
                    };
            in
              builder {
                pkgs = config.pkgs;
                lib = config.pkgs.lib;
                modules = config.modules;
                extraSpecialArgs =
                  {
                    homeModules =
                      if globalModules ? "home"
                      then globalModules.home
                      else {};
                  }
                  // config.args;
              };
          };
        };
      }));
    };

    generators.home = {
      username = lib.options.create {
        type = lib.types.string;
        description = "The username to use for all discovered home-manager hosts.";
      };
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover home-manager hosts.";
        default.value = null;
      };
      args = lib.options.create {
        description = "Additional arguments to pass to home-manager modules.";
        type = lib.types.attrs.any;
        default.value = {};
      };
      modules = lib.options.create {
        type = lib.types.list.of lib.types.raw;
        default.value = [];
        description = "Default modules to include in all hosts.";
      };
    };

    generators.homeModules = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover home-manager modules.";
        default.value = null;
      };
    };
  };

  config = {
    assertions =
      (lib.lists.when config.generators.assertPaths [
        {
          assertion =
            config.generators.home.folder
            == null
            || (config.generators.home.folder != null && pathExists config.generators.home.folder);
          message = "Home-Manager generator's folder \"${config.generators.home.folder}\" does not exist.";
        }
        {
          assertion =
            config.generators.homeModules.folder
            == null
            || (config.generators.homeModules.folder != null && pathExists config.generators.homeModules.folder);
          message = "Home-Manager modules generator's folder \"${config.generators.homeModules.folder}\" does not exist.";
        }
      ])
      ++ (lib.attrs.mapToList
        (name: value: {
          assertion = !(builtins.isNull value.pkgs);
          message = "A Nixpkgs instance is required for the home-manager configuration \"${name}\", but none was provided and \"inputs.nixpkgs\" does not exist.";
        })
        config.systems.home);

    # Generate home configurations from `generators.home`
    systems.home =
      lib.modules.when
      (config.generators.home.folder != null && pathExists config.generators.home.folder)
      (
        let
          inherit (builtins) readDir hasAttr filter attrNames;

          loadUsers' = dir: let
            users' = let
              contents = readDir dir;
            in
              if (hasAttr "home" contents) && (contents.home == "directory")
              then
                filter
                (n:
                  (builtins.match ".*\.nix" n != null)
                  && (readDir "${dir}/home")."${n}" == "regular")
                (attrNames (readDir "${dir}/home"))
              else [];
          in
            concatMap
            (n: let
              username = lib.strings.removeSuffix ".nix" n;
              homeDir = readDir "${dir}/home";
              hasConfig =
                (hasAttr n homeDir)
                && (homeDir."${n}" == "regular");
            in
              if hasConfig
              then [
                {
                  username = username;
                  configuration = import "${dir}/home/${n}";
                }
              ]
              else [])
            users';

          loadUsersFromHostDir = dir: let
            hosts' = let
              contents = readDir dir;
            in
              filter
              (n: contents."${n}" == "directory")
              (attrNames contents);
          in
            concatMap
            (
              n: let
                users = loadUsers' "${dir}/${n}";
              in
                if users != []
                then [
                  {
                    hostname = n;
                    users = users;
                  }
                ]
                else []
            )
            hosts';

          hosts = config.lib.utils.loadHostsFromDir config.generators.home.folder "home.nix";
          hostsWithUsers = loadUsersFromHostDir config.generators.home.folder;

          genHomeSystem = username: configuration: {
            args =
              {inherit (config) inputs;}
              // config.generators.home.args;
            modules =
              [
                configuration
                ({lib, ...}: {
                  home.username = lib.mkDefault username;
                  home.homeDirectory = lib.mkDefault "/home/${username}";
                })
              ]
              ++ config.generators.home.modules;
          };
        in
          listToAttrs (
            (
              # Generate from `<folder>/<hostname>/home.nix`
              map (host: {
                name = "${config.generators.home.username}@${host.hostname}";
                value = genHomeSystem config.generators.home.username host.configuration;
              })
              hosts
            )
            ++ (
              # Generate from `<folder>/<hostname>/home/<username>.nix`
              concatMap
              (host:
                map (user: {
                  name = "${user.username}@${host.hostname}";
                  value = genHomeSystem user.username user.configuration;
                })
                host.users)
              hostsWithUsers
            )
          )
      );

    # Generate home modules from `generators.homeModules`
    modules.home =
      lib.modules.when
      (config.generators.homeModules.folder != null && pathExists config.generators.homeModules.folder)
      (
        mapAttrs
        (_name: import)
        (
          lib.utils.loadDirsWithFile
          "default.nix"
          config.generators.homeModules.folder
        )
      );
  };
}
