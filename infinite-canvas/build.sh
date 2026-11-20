#!/bin/sh
set -eu

canvas_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
new_api_root=$(CDPATH= cd -- "$canvas_root/.." && pwd)
build_parent=$(mktemp -d)
build_dir="$build_parent/infinite-canvas"
trap 'if [ -n "${CANVAS_KEEP_BUILD:-}" ]; then printf "Canvas checkout: %s\n" "$build_dir"; else rm -rf -- "$build_parent"; fi' EXIT

if [ ! -f "$canvas_root/web/package.json" ] || [ ! -f "$canvas_root/UPSTREAM_REVISION" ]; then
  echo "Embedded Infinite Canvas source is incomplete" >&2
  exit 1
fi

# The source tree is already part of this repository. The temporary copy keeps
# generated dist files and dependency installs out of the tracked source tree.
mkdir -p "$build_dir" "$build_parent/web/src/features/playground/lib"
cp -R "$canvas_root"/. "$build_dir"/
cp "$new_api_root/web/src/features/playground/types.ts" "$new_api_root/web/src/features/playground/constants.ts" "$build_parent/web/src/features/playground/"
cp "$new_api_root/web/src/features/playground/lib/image-generation-capabilities.ts" "$new_api_root/web/src/features/playground/lib/image-payload-builder.ts" "$build_parent/web/src/features/playground/lib/"
(
  cd "$build_dir/web"
  bun install --frozen-lockfile
  bun run typecheck
  bun run build
)

mkdir -p "$new_api_root/web/dist/canvas-app"
cp -R "$build_dir/web/dist/." "$new_api_root/web/dist/canvas-app/"
cp "$build_dir/LICENSE" "$new_api_root/web/dist/canvas-app/LICENSE"
printf '%s\n' "$(cat "$canvas_root/UPSTREAM_REVISION")" > "$new_api_root/web/dist/canvas-app/REVISION"
