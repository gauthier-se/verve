{
  description = "Verve devshell";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "aarch64-darwin" "x86_64-linux" "aarch64-linux" ];
    in
    {
      devShells = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};

          # CI installs the latest 1.27 patch with actions/setup-go, nixpkgs
          # still has 1.27.3, which govulncheck flags (GO-2026-6859). Remove
          # this override once nixpkgs ships go_1_27 1.27.4 or later.
          go = pkgs.go_1_27.overrideAttrs (old: rec {
            version = "1.27.4";
            src = pkgs.fetchurl {
              url = "https://go.dev/dl/go${version}.src.tar.gz";
              hash = "sha256-c8+4VxrsDAEXdofbhxY9Rqog6XeWcncokaoXZU2aYJk=";
            };
          });

          # Go tools (task, air, sqlc, govulncheck) are pinned in go.mod and run
          # with `go tool`, so CI and contributors without Nix get the same
          # versions. This wrapper only keeps the command short.
          task = pkgs.writeShellScriptBin "task" ''
            if ! go tool -n task >/dev/null 2>&1; then
              echo "task is not available yet: it is a Go tool dependency declared in go.mod" >&2
              exit 1
            fi
            exec go tool task "$@"
          '';
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              gotools
              delve
              golangci-lint
              nodejs_26
              pnpm_12
              task
            ];

            # Use the Go from this shell, never a toolchain downloaded because
            # of the go directive in go.mod.
            GOTOOLCHAIN = "local";

            # Browsers downloaded by Playwright do not run on NixOS: use the
            # nixpkgs ones. @playwright/test must stay on the same version as
            # playwright-driver in flake.lock. macOS keeps Playwright's own.
            shellHook = pkgs.lib.optionalString pkgs.stdenv.hostPlatform.isLinux ''
              export PLAYWRIGHT_BROWSERS_PATH=${pkgs.playwright-driver.browsers}
              export PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=true
            '';
          };
        });
    };
}
