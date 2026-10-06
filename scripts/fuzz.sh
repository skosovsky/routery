#!/bin/sh
# Discover packages and exact targets without suppressing build/list failures.
set -eu
runner=${GO:-go}
duration=${FUZZTIME:-30s}
packages=$("$runner" list -tags=fuzz ./...)
for package in $packages; do
    listing=$("$runner" test -tags=fuzz -list '^Fuzz' "$package")
    targets=$(printf '%s\n' "$listing" | sed -n '/^Fuzz[A-Za-z0-9_]*$/p')
    for target in $targets; do
        echo "fuzz target - $package/$target"
        "$runner" test -tags=fuzz -run '^$' -fuzz "^${target}$" -fuzztime "$duration" "$package"
    done
done
