let
  pins = import ./npins;

  nilla = import pins.nilla;

  systems = [
    "x86_64-linux"
    "aarch64-linux"
    "aarch64-darwin"
  ];
in
nilla.create (
  { config, lib, ... }:
  let
    plugins = [
      "nilla-os"
      "nilla-home"
      "nilla-darwin"
      "nilla-microvm"
    ];

    # Expose each plugin as its own package so `nilla run <plugin>` can
    # find `bin/<plugin>`. These only symlink into `nilla-utils-plugins`,
    # so the Go code is still only built once.
    mkPlugin = name: {
      inherit systems;

      builder = "nixpkgs";
      settings.pkgs = config.inputs.nixpkgs.result;

      package =
        { stdenv, runCommand }:
        let
          all = config.packages.nilla-utils-plugins.result.${stdenv.hostPlatform.system};
        in
        runCommand name
          {
            inherit (all) version;
            meta = all.meta // {
              mainProgram = name;
            };
          }
          ''
            mkdir -p $out/bin
            ln -s ${all}/bin/${name} $out/bin/${name}
          '';
    };
  in
  {
    config = {
      inputs = {
        nixpkgs = {
          src = pins.nixpkgs;

          settings.overlays = [
            (
              final: prev:
              let
                callPackage = final.callPackage;
              in
              {
                inherit (callPackage "${pins.gomod2nix}/builder" { }) buildGoApplication mkGoEnv mkVendorEnv;
                gomod2nix = callPackage "${pins.gomod2nix}/default.nix" { };
              }
            )
          ];
        };
      };

      packages = lib.attrs.generate plugins mkPlugin // {
        default = config.packages.nilla-utils-plugins;
        nilla-utils-plugins = {
          inherit systems;

          builder = "nixpkgs";
          settings.pkgs = config.inputs.nixpkgs.result;

          package = import ./package.nix;
        };
      };

      shells.default = {
        inherit systems;

        builder = "nixpkgs";
        settings.pkgs = config.inputs.nixpkgs.result;

        shell =
          {
            mkShellNoCC,
            npins,
            gomod2nix,
            nvd,
            ...
          }:
          mkShellNoCC {
            packages = [
              npins
              gomod2nix
              nvd
            ];
          };
      };
    };
  }
)
