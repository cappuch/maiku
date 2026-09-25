# Updates

Official release builds check GitHub Releases for the repository embedded at
build time. Local builds (`Version=dev`, `BuildTime=0`) do not update themselves.
The current release matrix is supported: macOS universal desktop, Windows and
Linux amd64 desktop, and CLI amd64/arm64 on macOS, Windows, and Linux. Unsupported
targets return an error instead of installing a different architecture.

- Desktop checks after the window loads and every six hours. Accept the native
  Yes/No prompt to install, or use **Help → Check for Updates…**. Sessions continue;
  quit and reopen the app to run the installed version.
- **Settings → Updates → Auto update** is checked by default. Changes save
  immediately to the global `autoUpdate` preference in `settings.json`, persist
  across restarts, and control both desktop and CLI automatic updates. Workspace
  settings cannot override this preference. Manual checks remain available.
- The CLI checks and installs before an interactive launch with no arguments.
  `maiku update` installs explicitly; `maiku update --check` only checks.
  Launches with arguments, non-terminal input/output, help, and version do not
  trigger automatic updates. An automatic failure does not prevent startup.
- Set `MAIKU_AUTO_UPDATE=0` to disable automatic checks and installation. Manual
  checks still work. Install directories must be writable by the current user;
  the updater never requests elevated privileges. On macOS, install the `.app`
  in a writable Applications directory before updating (not inside a disk image).

## Release contract

`.github/workflows/release.yml` embeds `updater.Version`, `updater.BuildTime`
(the commit's Unix timestamp), and `updater.Repository` in every executable.
It publishes `updater.json` containing `version` and `buildTime`, and SHA-256
`checksums.txt` alongside the platform artifacts. The same JSON is embedded in
the release body as `<!-- maiku-updater:{...} -->` so checks need only one API
request. Releases stay drafts until
all assets are uploaded. Commit prereleases are included because this project
releases each main-branch commit. Only strictly newer commit timestamps qualify;
republishing an old commit does not downgrade an installation. The updater scans
the latest 100 releases. Releases without the required files are ignored.

Downloads use HTTPS and fixed repository release URLs, verify the artifact's
SHA-256, and enforce download/extraction limits. Archives cannot write outside
staging or contain links/special files. This trusts GitHub and repository release
permissions; checksums are integrity checks, not independent publisher signatures.
No GitHub token is embedded. Private repositories require a public distribution
repository/feed; unauthenticated clients cannot fetch their releases.

## Installation and recovery

Staging and backup directories are created next to the installed executable or
macOS `.app`, so renames stay on the same filesystem. Symlinks to executables are
resolved. The whole macOS bundle is replaced. Windows renames the old executable
before placing the new one; if antivirus or permissions prevent this, the update
fails. A failed final rename restores the original installation. No forced
restart occurs. Multiple processes serialize installation using an OS lock on
the sibling `<installation>.update-lock` file. The file stays in place, but the
OS releases the lock on exit or crash.
A sibling `.update-version` receipt prevents an older running process from
reinstalling an update that another process has already installed.

An abrupt termination or power loss during installation may leave
a `.maiku-update-*` staging directory behind. After closing all maiku processes,
if the original installation is absent, move `previous`
from that staging directory back to the original installation path. A rollback
failure reports both paths. Windows may retain `previous` while the old executable
is running; leftover staging directories can be removed after all old processes
exit. Do not remove backups while an installation or recovery is in progress.

Platform CI should exercise installation on each native runner. Cross-compiling
alone cannot verify Windows file locking, macOS bundle behavior, or native dialogs.
