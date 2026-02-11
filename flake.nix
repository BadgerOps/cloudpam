{
  description = "CloudPAM – IP Address Management platform";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Derive version from the git revision so every build is traceable.
        version =
          if self ? rev
          then self.rev
          else self.dirtyRev or "dirty";

        shortRev =
          if self ? shortRev
          then self.shortRev
          else "dirty";
      in
      {
        packages = {
          default = pkgs.buildGoModule.override { go = pkgs.go_1_24; } {
            pname = "cloudpam";
            inherit version;
            src = self;

            vendorHash = "sha256-VXVgoHG6LYGJtix45h439OZTg/UHQtco0MSsoma6GJc=";

            tags = [ "sqlite" ];
            ldflags = [
              "-s" "-w"
              "-X main.version=${version}"
              "-X main.gitCommit=${shortRev}"
            ];

            env.CGO_ENABLED = 0;

            subPackages = [ "cmd/cloudpam" ];

            meta = with pkgs.lib; {
              description = "Intelligent IP Address Management platform";
              homepage = "https://github.com/BadgerOps/cloudpam";
              license = licenses.mit;
              mainProgram = "cloudpam";
            };
          };

          # OCI / Docker image built from the Nix derivation.
          docker = pkgs.dockerTools.buildLayeredImage {
            name = "cloudpam";
            tag = shortRev;
            contents = [ self.packages.${system}.default pkgs.cacert pkgs.tzdata ];
            config = {
              Entrypoint = [ "/bin/cloudpam" ];
              ExposedPorts."8080/tcp" = {};
              Env = [ "TZ=UTC" ];
            };
          };
        };

        # Development shell with Go, Just, linting tools, and git-aware prompt.
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_24
            just
            golangci-lint
            gopls
            gotools
            delve
            docker-client
            git
          ];

          shellHook = ''
            # Git-aware PS1: shows branch, short commit, and dirty state.
            __cloudpam_ps1() {
              local branch commit dirty
              branch=$(git symbolic-ref --short HEAD 2>/dev/null || git describe --tags --exact-match 2>/dev/null || git rev-parse --short HEAD 2>/dev/null)
              if [ -n "$branch" ]; then
                commit=$(git rev-parse --short HEAD 2>/dev/null)
                dirty=""
                if ! git diff --quiet HEAD -- 2>/dev/null; then
                  dirty="*"
                fi
                echo " ($branch@$commit$dirty)"
              fi
            }
            export PS1='\[\033[1;34m\]cloudpam\[\033[0;33m\]$(__cloudpam_ps1)\[\033[0m\] \w \$ '
            echo "cloudpam dev shell – Go $(go version | awk '{print $3}')"
          '';
        };
      }
    );
}
