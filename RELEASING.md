# Releasing Lense

A release is built, signed with the Developer ID of `mygo.json` and notarized
by Apple on GitHub, for Apple silicon and as a universal app for Intel Macs
too, into a draft release, which is published once it is checked.

1. Set the new `version` in `mygo.json`. A patch (1.0.0 -> 1.0.1) for bug fixes, small changes and minor for features.
2. Date its section in `CHANGELOG.md`: `## 0.2.0 - 2026-11-30` at the top and
   write what has changed since the last release (`git log vX.Y.Z..HEAD`) for
   people who use Lense, not for developers. Name settings as they are displayed
   in the app (Settings › Security), credit outside contributors (Thanks @name).
   Leave out unnoticable changes and fixes that were not confirmed to work.
3. Commit them as `Release X.Y.Z`, tag the commit `vX.Y.Z`, and push `main`
   with the tag:

   ```sh
   git commit -am "Release X.Y.Z"
   git tag vX.Y.Z
   git push origin main
   git push origin vX.Y.Z
   ```

   The [Release](.github/workflows/release.yml) workflow drafts the release
   of the tag, and uploads the builds to it. It signs and notarizes with the
   secrets of the `macOS Distribution` environment, which admits tags of a
   version (`v*.*.*`) alone: a run by hand on a branch can't sign.
4. Wait for the release to finish:

   ```sh
   gh run list --workflow release.yml --limit 1
   gh run watch <run id> --exit-status
   ```

5. Once the Release run succeeded, check the draft on GitHub: the disk images,
   for Apple silicon and universal, and MyGo's update archives. Give it the
   notes of the version's section in `CHANGELOG.md`.
6. Publish it: run the [Publish release](.github/workflows/publish-release.yml)
   workflow on `main`, or

   ```sh
   gh release edit vX.Y.Z --draft=false
   ```

The release holds the signed update archives that MyGo's upload makes (see
[auto-updates](https://github.com/egoist/mygo/blob/main/docs/updates.md)):
Lense itself never looks for them.
