# Source this file: source setup.sh
# Sets up the environment for running aat against the travelport project.
#
# Prerequisites:
#   1. Build AAT:           make build
#   2. Clone travelport:    git clone https://github.com/gburgyan/aat-graph-travelport.git travelport
#   3. Set your API key:    export OPENAI_API_KEY="your-key-here"

# Root of the aat repository (where this script lives)
AAT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Add the aat binary (built in repo root) to PATH
export PATH="$AAT_ROOT:$PATH"

# Point aat at the travelport project manifest
export AAT_PROJECT="$AAT_ROOT/travelport"

# OpenAI API key for LLM-assisted planning
if [ -z "$OPENAI_API_KEY" ]; then
  echo "Warning: OPENAI_API_KEY is not set. LLM features will not work."
  echo "Set it with: export OPENAI_API_KEY=\"your-key-here\""
fi
