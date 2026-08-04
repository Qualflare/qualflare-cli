#!/usr/bin/env bash
# Publish the npm set built by scripts/build-npm-packages.mjs.
#
#   scripts/publish-npm-packages.sh <version> [--dry-run]
#
# Order is load-bearing: every platform package first, the main package last. The main
# package pins its optionalDependencies to exact versions, so publishing it first would
# briefly point consumers at tarballs that do not exist yet.
#
# Idempotent — a version already on the registry is skipped rather than failing the run,
# so a release that died partway through is repaired by re-running this.

set -euo pipefail

VERSION="${1:?usage: publish-npm-packages.sh <version> [--dry-run]}"
DRY_RUN="${2:-}"

DIST="${DIST:-dist}"
OUT="$DIST/npm"

if [[ ! -d "$OUT" ]]; then
  echo "error: $OUT not found — run scripts/build-npm-packages.mjs $VERSION first" >&2
  exit 1
fi

# A prerelease must not land on the `latest` dist-tag, or `npm install @qualflare/cli`
# starts handing out release candidates.
case "$VERSION" in
  *-*) TAG="next" ;;
  *)   TAG="latest" ;;
esac

publish() {
  local dir="$1"
  local name
  name="$(node -p "require('./$dir/package.json').name")"

  if npm view "$name@$VERSION" version >/dev/null 2>&1; then
    echo "skip    $name@$VERSION (already published)"
    return
  fi

  echo "publish $name@$VERSION --tag $TAG"
  # --access/--provenance also live in each package.json's publishConfig; passing them
  # explicitly keeps the behaviour obvious at the call site.
  npm publish "$dir" --access public --provenance --tag "$TAG" $DRY_RUN
}

# Platform packages first...
for dir in "$OUT"/@qualflare/cli-*/; do
  publish "${dir%/}"
done

# ...main package last.
publish "$OUT/@qualflare/cli"

echo "done."
