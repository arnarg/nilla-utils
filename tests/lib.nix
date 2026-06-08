let
  pins = import ../npins;

  nilla = import pins.nilla;

  resolved = nilla.create {
    includes = [
      ../modules
    ];
  };

  lib = resolved.lib;
in
{
  loadDirsWithFile = {
    testEmpty = {
      expr = lib.utils.loadDirsWithFile "default.nix" ./dir/empty;
      expected = { };
    };
    testMultiple = {
      expr = lib.utils.loadDirsWithFile "default.nix" ./dir/two;
      expected = {
        first = ./dir/two/first;
        second = ./dir/two/second;
      };
    };
  };
  loadDirsWithFileRecursive = {
    testEmpty = {
      expr = lib.utils.loadDirsWithFileRecursive "default.nix" ./dir/empty;
      expected = { };
    };
    testNested = {
      expr = lib.utils.loadDirsWithFileRecursive "default.nix" ./dir/recursive;
      expected = {
        first = ./dir/recursive/first;
        very = {
          default = ./dir/recursive/very;
          nested.second = ./dir/recursive/very/nested/second;
        };
        third = ./dir/recursive/third;
        fourth.is.very.nested = ./dir/recursive/fourth/is/very/nested;
      };
    };
  };
}
