#!/bin/sh
command -v imprint-dev >/dev/null 2>&1 || exit 0
exec imprint-dev hook-skill-suggestion
