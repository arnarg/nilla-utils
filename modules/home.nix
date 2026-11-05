{ config }:
let
  inherit (config) inputs lib;
  inherit (builtins)
    listToAttrs
    mapAttrs
    pathExists
    concatMap
    ;

  globalModules = config.modules;

  homeFolder = config.generators.home.folder;
  nixosFolder = config.generators.nixos.folder;

  homeGeneratorEnabled = homeFolder != null && pathExists homeFolder;

  generatedArgs = {
    inherit (config) inputs;
  }
  // config.generators.home.args;

  # Whether a host's home users should be built into its NixOS system.
  nixosSelection = config.generators.home.nixos;

  hasNixosConfig =
    hostname: nixosFolder != null && pathExists "${nixosFolder}/${hostname}/configuration.nix";

  # Whether a list entry ("host" or "user@host") selects a discovered user.
  entryMatches = u: entry: entry == u.hostname || entry == "${u.username}@${u.hostname}";

  # Whether a discovered user should be built into its host's NixOS system.
  isNixosUser =
    u:
    hasNixosConfig u.hostname
    && (
      if builtins.isBool nixosSelection then
        nixosSelection
      else
        builtins.any (entryMatches u) nixosSelection
    );

  # All discovered users as a list of { hostname, username, configuration }.
  # From `<folder>/<hostname>/home.nix` and `<folder>/<hostname>/home/<username>.nix`.
  discoveredUsers =
    let
      inherit (builtins)
        readDir
        filter
        attrNames
        match
        ;

      hostDirs =
        let
          contents = readDir homeFolder;
        in
        filter (n: contents.${n} == "directory") (attrNames contents);

      usersInHost =
        hostname:
        let
          dir = "${homeFolder}/${hostname}";
          contents = readDir dir;

          single = lib.lists.when (contents ? "home.nix" && contents."home.nix" == "regular") [
            {
              inherit hostname;
              username = config.generators.home.username;
              configuration = import "${dir}/home.nix";
            }
          ];

          multi =
            if contents ? home && contents.home == "directory" then
              let
                homeDir = readDir "${dir}/home";
              in
              map (n: {
                inherit hostname;
                username = lib.strings.removeSuffix ".nix" n;
                configuration = import "${dir}/home/${n}";
              }) (filter (n: match ".*\\.nix" n != null && homeDir.${n} == "regular") (attrNames homeDir))
            else
              [ ];
        in
        single ++ multi;
    in
    concatMap usersInHost hostDirs;
in
{
  includes = [
    ./lib.nix
  ];

  options = {
    systems.home = lib.options.create {
      description = "home-manager systems to create.";
      default.value = { };
      type = lib.types.attrs.of (
        lib.types.submodule (
          { config }:
          {
            options = {
              args = lib.options.create {
                description = "Additional arguments to pass to home-manager modules.";
                type = lib.types.attrs.any;
                default.value = { };
              };

              system = lib.options.create {
                description = "The system of pkgs to use.";
                type = lib.types.string;
                default.value = "x86_64-linux";
              };

              home-manager = lib.options.create {
                description = "The home-manager input to use.";
                type = lib.types.raw;
                default.value = if inputs ? home-manager then inputs.home-manager else null;
              };

              pkgs = lib.options.create {
                description = "The Nixpkgs instance to use.";
                type = lib.types.raw;
                default.value =
                  if inputs ? nixpkgs && inputs.nixpkgs.result ? ${config.system} then
                    inputs.nixpkgs.result.${config.system}
                  else
                    null;
              };

              modules = lib.options.create {
                description = "A list of modules to use for home-manager.";
                type = lib.types.list.of lib.types.raw;
                default.value = [ ];
              };

              result = lib.options.create {
                description = "The created home-manager system.";
                type = lib.types.raw;
                writable = false;
                default.value =
                  let
                    src = config.home-manager.src;
                    contents = builtins.readDir src;
                    directories = lib.attrs.filter (name: value: value == "directory") contents;

                    builder =
                      if directories ? "lib" && (builtins.readDir "${src}/lib") ? "default.nix" then
                        (import "${src}/lib" { inherit (config.pkgs) lib; }).homeManagerConfiguration
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
                          configuration =
                            { lib, ... }:
                            {
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
                    extraSpecialArgs = {
                      homeModules = if globalModules ? "home" then globalModules.home else { };
                    }
                    // config.args;
                  };
              };
            };
          }
        )
      );
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
        default.value = { };
      };
      modules = lib.options.create {
        type = lib.types.list.of lib.types.raw;
        default.value = [ ];
        description = "Default modules to include in all hosts.";
      };
      nixos = lib.options.create {
        type = lib.types.either lib.types.bool (lib.types.list.of lib.types.string);
        default.value = false;
        description = ''
          Build discovered home-manager users into the NixOS system of the same
          host (using the home-manager NixOS module) instead of creating
          standalone `systems.home` entries.

          `true` selects all users, `false` none. A list selects users by
          `"host"` (every user on that host) or `"user@host"` (one user).

          Only applies to hosts that also have a `configuration.nix` in
          `generators.nixos.folder`; other users stay standalone.
        '';
      };
    };

    generators.homeModules = {
      folder = lib.options.create {
        type = lib.types.nullish lib.types.path;
        description = "The folder to auto discover home-manager modules.";
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
    assertions =
      (lib.lists.when config.generators.assertPaths [
        {
          assertion =
            config.generators.home.folder == null
            || (config.generators.home.folder != null && pathExists config.generators.home.folder);
          message = "Home-Manager generator's folder \"${config.generators.home.folder}\" does not exist.";
        }
        {
          assertion =
            config.generators.homeModules.folder == null
            || (
              config.generators.homeModules.folder != null && pathExists config.generators.homeModules.folder
            );
          message = "Home-Manager modules generator's folder \"${config.generators.homeModules.folder}\" does not exist.";
        }
      ])
      ++ (lib.attrs.mapToList (name: value: {
        assertion = !(builtins.isNull value.pkgs);
        message = "A Nixpkgs instance is required for the home-manager configuration \"${name}\", but none was provided and \"inputs.nixpkgs\" does not exist.";
      }) config.systems.home)
      ++ (lib.lists.when (homeGeneratorEnabled && builtins.isList nixosSelection) (
        map (entry: {
          assertion = builtins.any (u: entryMatches u entry && hasNixosConfig u.hostname) discoveredUsers;
          message = "\"${entry}\" in \"generators.home.nixos\" does not match any discovered home-manager user on a host with a \"configuration.nix\" in \"generators.nixos.folder\".";
        }) nixosSelection
      ));

    # Generate standalone home configurations from `generators.home`
    systems.home = lib.modules.when homeGeneratorEnabled (
      listToAttrs (
        map (u: {
          name = "${u.username}@${u.hostname}";
          value = {
            args = generatedArgs;
            modules = [
              u.configuration
              (
                { lib, ... }:
                {
                  home.username = lib.mkDefault u.username;
                  home.homeDirectory = lib.mkDefault "/home/${u.username}";
                }
              )
            ]
            ++ config.generators.home.modules;
          };
        }) (builtins.filter (u: !(isNixosUser u)) discoveredUsers)
      )
    );

    # Build discovered home users into NixOS systems when `generators.home.nixos` is set
    systems.nixos = lib.modules.when homeGeneratorEnabled (
      let
        nixosUsers = builtins.filter isNixosUser discoveredUsers;
        hostnames = lib.lists.unique (map (u: u.hostname) nixosUsers);
      in
      listToAttrs (
        map (hostname: {
          name = hostname;
          value.home = {
            args = generatedArgs;
            users = listToAttrs (
              map (u: {
                name = u.username;
                value.imports = [ u.configuration ] ++ config.generators.home.modules;
              }) (builtins.filter (u: u.hostname == hostname) nixosUsers)
            );
          };
        }) hostnames
      )
    );

    # Generate home modules from `generators.homeModules`
    modules.home =
      let
        loader =
          if config.generators.homeModules.recursive then
            lib.utils.loadDirsWithFileRecursive
          else
            lib.utils.loadDirsWithFile;
      in
      lib.modules.when (
        config.generators.homeModules.folder != null && pathExists config.generators.homeModules.folder
      ) (lib.utils.importTree (loader "default.nix" config.generators.homeModules.folder));
  };
}
