# nilla-utils

[Nilla](https://github.com/nilla-nix/nilla) modules and cli plugins to work with NixOS and home-manager configurations.

## Overview

`nilla-utils` enhances your [Nilla](https://github.com/nilla-nix/nilla) experience by providing:

*   **Simplified NixOS & Home Manager Definitions:** Easily declare and manage your system configurations directly within Nilla.
*   **Powerful Configuration Generators:** Automate the creation of Nilla inputs, packages, shells, overlays, and even full NixOS/Home Manager systems from your project's directory structure.
*   **Convenient CLI Plugins:** Extend the `nilla` command-line tool with `nilla os` and `nilla home` subcommands for building, switching, and managing your NixOS and Home Manager generations, including diffing and remote deployment capabilities.

# Table of contents

- [Quickstart](#quickstart)
- [NixOS](#nixos)
- [Home Manager](#home-manager)
- [Nilla cli plugins](#nilla-cli-plugins)
- [Generators](#generators)
  - [Inputs](#inputs)
  - [Packages](#packages)
  - [Shells](#shells)
  - [Overlays](#overlays)
  - [NixOS](#nixos-2)
  - [Home Manager](#home-manager-2)
- [Examples](#examples)

## Quickstart

If you're using `npins` with your Nilla configuration, add nilla-utils as a dependency:

```sh
npins add github -b main arnarg nilla-utils
```

And import the modules to your nilla configuration.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    inputs.nilla-utils.src = pins.nilla-utils;

    # ...
  };
})
```

## NixOS

The NixOS module adds support for NixOS systems under `systems.nixos`.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    systems.nixos.mysystem = {
      # Pass args to NixOS modules.
      args.inputs = config.inputs;

      # Set system from nilla.
      # This option has lower priority than the NixOS option
      # `nixpkgs.hostPlatform` within the NixOS modules.
      system = "x86_64-linux";

      modules = [
        {
          networking.hostName = "mysystem";

          # ...
        }
      ];
    };

    # ...
  };
})
```

## Home Manager

The Home Manager module adds support for Home Manager systems under `systems.home`.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    systems.home."user@system1" = {
      # Pass args to home-manager modules.
      args.inputs = config.inputs;

      modules = [
        {
          home.username = "user";
          home.homeDirectory = "/home/user";

          # ...
        }
      ];
    };

    # ...
  };
})
```

## Nilla cli plugins

nilla-utils provides plugins for nilla cli for sub-commands `nilla os` and `nilla home` that can be used to build and switch to NixOS and home-manager systems.

To install the plugins the following can be added to your NixOS or home-manager modules (provided that `args.inputs = config.inputs;` from the examples above are added).

### NixOS

```nix
{inputs, ...}: {
  environment.systemPackages = [
    # Added with `npins add --name nilla-cli github -b main nilla-nix cli`
    inputs.nilla-cli.packages.default.result.x86_64-linux
    inputs.nilla-utils.packages.default.result.x86_64-linux
  ];
}
```

### Home Manager

```nix
{inputs, ...}: {
  home.packages = [
    # Added with `npins add --name nilla-cli github -b main nilla-nix cli`
    inputs.nilla-cli.packages.default.result.x86_64-linux
    inputs.nilla-utils.packages.default.result.x86_64-linux
  ];
}
```
### Using the CLI Plugins

Once installed, you can use the following commands:

#### NixOS (`nilla os`)

For managing NixOS systems defined in `systems.nixos`.

*   **Build a configuration:**
    ```sh
    nilla os build <system_name>
    ```
*   **Build and switch to a configuration:**
    ```sh
    nilla os switch <system_name>
    # For remote targets:
    # nilla os switch <system_name> --target user@hostname
    ```
*   **Test a configuration:**
    ```sh
    nilla os test <system_name>
    ```
*   **Make configuration boot default:**
    ```sh
    nilla os boot <system_name>
    ```
*   **List available NixOS configurations:**
    ```sh
    nilla os list
    ```
*   **Manage generations:**
    ```sh
    nilla os generations list
    nilla os generations clean --keep 3 # Keeps the last 3 generations
    ```
    Use `nilla os --help` or `nilla os <subcommand> --help` for more details.

#### Home Manager (`nilla home`)

For managing Home Manager configurations defined in `systems.home`.

*   **Build a configuration:**
    ```sh
    nilla home build <user@system_name>
    ```
*   **Build and switch to a configuration:**
    ```sh
    nilla home switch <user@system_name>
    ```
*   **List available Home Manager configurations:**
    ```sh
    nilla home list
    ```
*   **Manage generations:**
    ```sh
    nilla home generations list
    nilla home generations clean --keep 3 # Keeps the last 3 generations
    ```
    Use `nilla home --help` or `nilla home <subcommand> --help` for more details.

## Generators

`nilla-utils` modules include powerful generators that automate the creation of Nilla configurations by discovering files and structures within your project. This reduces boilerplate and encourages a consistent project layout.

### Inputs

The inputs generator will generate `config.inputs.*` from your npins.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    # Load all pins from npins and generate nilla inputs.
    generators.inputs.pins = pins;

    # Individual inputs can be overwritten in the standard inputs.
    inputs = {
      # Set nixpkgs config
      nixpkgs.settings.configuration = {
        allowUnfree = true;
      };

      # Set nilla loader for nilla-utils (although redundant).
      nilla-utils.loader = "nilla";
    };

    # ...
  };
})
```

### Packages

The packages generator will generate `config.packages.*` from folders containing a `default.nix` from a specified folder.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    # Generate packages from sub-folders in `./packages`.
    generators.packages.folder = ./packages;

    # ...
  };
})
```

Now `nilla build mypackage` will build the package in `./packages/mypackage/default.nix`.


### Shells

The shells generator will generate `config.shells.*` from folders containing a `default.nix` from a specified folder.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    # Generate shells from sub-folders in `./shells`.
    generators.shells.folder = ./shells;

    # ...
  };
})
```

Now `nilla shell myshell` will enter the shell in `./shells/myshell/default.nix`.

### Overlays

The overlays generator will generate `config.overlays.*` from folders containing a `default.nix` from a specified folder.

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    # Generate overlay `default` with packages in `./packages`.
    generators.overlays.default.folder = ./packages;

    # Use the generated overlay
    inputs.nixpkgs.setting.overlays = [config.overlays.default];

    # ...
  };
})
```

Now `config.overlays.default` set to an overlay adding all the packages found in `./packages/*/default.nix`.

### NixOS

The NixOS generator will generate NixOS systems from a directory with sub-directories of hosts containing a `configuration.nix` file.

Given the following structure:

```
.
├── hosts
│   ├── system1
│   │   ├── configuration.nix
│   │   └── hardware-configuration.nix
│   ├── system2
│   │   ├── configuration.nix
│   │   └── hardware-configuration.nix
│   └── something-else
│       └── default.nix
└── nilla.nix
```

And the following `nilla.nix` will generate NixOS systems for `system1` and `system2`:

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    generators.nixos = {
      # Set the folder to generate from.
      folder = ./hosts;

      # Pass args to NixOS modules.
      # The generator will automatically pass `config.inputs`
      # as `inputs`.
      # args.inputs = config.inputs;

      modules = [
        {
          # `networking.hostName` is automatically set
          # to the name of the sub-directory, i.e. system1 and system2.

          # ...
        }
      ];
    };

    # ...
  };
})
```

### Home Manager

The Home Manager generator will generate Home Manager systems from a directory with sub-directories of hosts containing a `home.nix` file.

Given the following structure:

```
.
├── hosts
│   ├── system1
│   │   └── home.nix
│   ├── system2
│   │   ├── configuration.nix
│   │   └── hardware-configuration.nix
│   └── something-else
│       └── default.nix
└── nilla.nix
```

And the following `nilla.nix` will generate Home Manager system `user@system1`:

```nix
# nilla.nix
let
  pins = import ./npins;

  nilla = import pins.nilla;
in nilla.create ({config}: {
  includes = [
    "${pins.nilla-utils}/modules"
  ];

  config = {
    generators.home = {
      # Set the folder to generate from.
      folder = ./hosts;

      # User to set in all generated systems.
      username = "user";

      # Pass args to home-manager modules.
      # The generator will automatically pass `config.inputs`
      # as `inputs`.
      # args.inputs = config.inputs;

      modules = [
        {
          # `home.username` and `home.homeDirectory` are automatically
          # generated from `generators.home.username`.

          # ...
        }
      ];
    };

    # ...
  };
})
```

## Examples

You can find more detailed examples of how to use `nilla-utils` in the [`examples/`](./examples) directory of this repository.

## Contributing

Contributions are welcome! Please feel free to open an issue or submit a pull request.

## License

`nilla-utils` is licensed under the MIT License. See the [LICENSE](./LICENSE) file for details.
