let
  pins = import ./npins;

  nilla = import pins.nilla;

  systems = ["x86_64-linux" "aarch64-linux"];
in
  nilla.create ({config, ...}: {
    config = {
      inputs = {
        nixpkgs = {
          src = pins.nixpkgs;

          settings.overlays = [
            (import "${pins.gomod2nix}/overlay.nix")
          ];
        };
      };

      packages.default = config.packages.nilla-utils-plugins;
      packages.nilla-utils-plugins = {
        inherit systems;

        builder = "nixpkgs";
        settings.pkgs = config.inputs.nixpkgs.result;

        package = import ./default.nix;
      };

      shells.default = {
        inherit systems;

        builder = "nixpkgs";
        settings.pkgs = config.inputs.nixpkgs.result;

        shell = {
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
  })
