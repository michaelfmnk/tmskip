# Releasing

1. Tag `vX.Y.Z` and push the tag. The `release` workflow builds the universal
   binary, signs and notarizes it, and publishes the GitHub release and the
   Homebrew formula. The secrets it needs are listed at the top of
   `.github/workflows/release.yml`, and the `michaelfmnk/homebrew-tap` repo must exist.
2. `brew update && brew upgrade tmskip` (or install), then check Homebrew didn't re-sign it:
   ```sh
   codesign -dv "$(brew --prefix)/bin/tmskip"     # must show the Developer ID TeamIdentifier
   codesign --verify --strict "$(brew --prefix)/bin/tmskip"
   spctl -a -t install "$(brew --prefix)/bin/tmskip"
   ```
3. Run `tmskip doctor` on a machine with the schedule on.
