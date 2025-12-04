#!/bin/bash
# ===============================================================
# 🚀 go-machine Backtester Setup Script
# ===============================================================

set -e

echo "🔧 Setting up backtester folder structure..."

# Create directories
# mkdir -p internal/backtest
# mkdir -p cmd/backtest

# # Create files
# touch internal/backtest/params.go
# touch internal/backtest/universe.go
# touch internal/backtest/engine.go
# touch internal/backtest/report.go
# touch cmd/backtest/main.go

# # Print structure
# echo "✅ Folder structure created:"
# tree -L 3 | grep -E "cmd|internal"

# echo ""
# echo "📁 Files created:"
# ls -1 internal/backtest cmd/backtest

# echo ""
# echo "✨ Done! Next steps:"
# echo "1. Copy and paste the code I provided for each file."
# echo "2. Run your backtester with:"
echo "   go run cmd/backtest/main.go -start 2025-09-20T00:00:00Z -end 2025-09-27T00:00:00Z -tf 5m -side short -universe core3"
echo ""
echo "🚀 All set! Ready to integrate the backtester."

