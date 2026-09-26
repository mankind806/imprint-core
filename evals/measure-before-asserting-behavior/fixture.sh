#!/usr/bin/env bash
# Seeds a throwaway dependency file with a pinned version for the run to check.
set -euo pipefail
echo "sample-lib==4.2.0" > requirements.txt
