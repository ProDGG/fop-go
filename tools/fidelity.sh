#!/bin/sh
# Compare fop-go with Apache FOP on testdata/fidelity/*.fo (or the given files).
# Needs: java, a FOP 2.x classpath in $FOPLIBS (dir with jars, see tools/README.md), poppler-utils.
# Usage: FOPLIBS=/path/libs FOPCONF=/path/fop.xconf tools/fidelity.sh [file.fo ...]
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${OUT:-/tmp/fidelity}
mkdir -p "$OUT"
[ -d "$FOPLIBS" ] || { echo "set FOPLIBS to a directory with Apache FOP jars"; exit 2; }
files=${*:-$ROOT/testdata/fidelity/*.fo}
go build -o "$OUT/fop-go" "$ROOT/cmd/fop-go"
for f in $files; do
  n=$(basename "$f" .fo)
  d=$(dirname "$f")
  java -Djava.awt.headless=true -cp "$FOPLIBS/*" org.apache.fop.cli.Main ${FOPCONF:+-c "$FOPCONF"} -fo "$f" -pdf "$OUT/$n.ref.pdf" >"$OUT/$n.ref.log" 2>&1 || echo "FOP failed on $n (see $OUT/$n.ref.log)"
  "$OUT/fop-go" -q -fo "$f" -pdf "$OUT/$n.pdf"
  printf '%-22s ' "$n"
  python3 "$ROOT/tools/comparepdf.py" "$OUT/$n.pdf" "$OUT/$n.ref.pdf" | tail -2 | tr '\n' ' '; echo
done
