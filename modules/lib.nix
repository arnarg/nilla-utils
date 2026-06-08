{ config }:
let
  inherit (builtins)
    attrNames
    concatMap
    filter
    foldl'
    hasAttr
    listToAttrs
    readDir
    ;
in
{
  config.lib.utils = {
    loadDirsWithFile =
      file:
      let
        loadDirsCond =
          f: dir:
          let
            contents = readDir dir;
          in
          listToAttrs (
            map (n: {
              name = n;
              value = dir + "/${n}";
            }) (filter (n: contents."${n}" == "directory" && f dir n) (attrNames contents))
          );
      in
      loadDirsCond (
        d: n:
        let
          contents = readDir "${d}/${n}";
        in
        (hasAttr file contents) && (contents.${file} == "regular")
      );

    loadDirsWithFileRecursive =
      file: baseDir:
      let
        recurse =
          dir:
          let
            contents = readDir dir;
            subdirs = filter (n: contents.${n} == "directory") (attrNames contents);

            process =
              acc: subdir:
              let
                childPath = dir + "/${subdir}";
                childResult = recurse childPath;
                hasFile =
                  let
                    childContents = readDir childPath;
                  in
                  (hasAttr file childContents) && (childContents.${file} == "regular");

                isEmpty = childResult == { };
              in
              if isEmpty && !hasFile then
                acc
              else if isEmpty && hasFile then
                acc // { ${subdir} = childPath; }
              else if hasFile then
                acc
                // {
                  ${subdir} = childResult // {
                    default = childPath;
                  };
                }
              else
                acc // { ${subdir} = childResult; };
          in
          foldl' process { } subdirs;
      in
      recurse baseDir;

    loadHostsFromDir =
      dir: file:
      let
        hosts' =
          let
            contents = readDir dir;
          in
          filter (n: contents."${n}" == "directory") (attrNames contents);
      in
      concatMap (
        n:
        let
          contents = readDir "${dir}/${n}";
          hasConfig = (hasAttr file contents) && (contents.${file} == "regular");
        in
        if hasConfig then
          [
            {
              hostname = n;
              configuration = import "${dir}/${n}/${file}";
            }
          ]
        else
          [ ]
      ) hosts';
  };
}
