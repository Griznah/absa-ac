#!/bin/bash
# Cleanup script for test artifacts and leftovers
# Usage: ./test_cleanup.sh [--dry-run]

set -eo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
    echo "=== DRY RUN MODE - No files will be deleted ==="
    echo ""
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

echo "Cleaning up test artifacts in: $REPO_ROOT"
echo ""

# Counters
FILES_CLEANED=0
DIRS_CLEANED=0

# Function to remove file or directory with dry-run support
remove_item() {
    local item="$1"
    local description="$2"

    if [[ ! -e "$item" ]]; then
        return
    fi

    # Check if it's a directory before removing (for accurate counting)
    local is_dir=0
    [[ -d "$item" ]] && is_dir=1

    if [[ "$DRY_RUN" == true ]]; then
        echo "[WOULD REMOVE] $description: $item"
    else
        rm -rf "$item"
        echo "[REMOVED] $description: $item"
    fi

    if [[ $is_dir -eq 1 ]]; then
        ((DIRS_CLEANED++))
    else
        ((FILES_CLEANED++))
    fi
}

# 1. Session key files (from proxy tests)
echo "=== Session Keys ==="
remove_item ".session_key" "Session encryption key" || true

# Find and remove session_key files in test temp directories
while IFS= read -r -d '' file; do
    [[ -n "$file" ]] && remove_item "$file" "Test encryption key" || true
done < <(find . -type f -name "test_key" -path "*/tmp/*" -print0 2>/dev/null || true)

# 2. Go test coverage files
echo ""
echo "=== Go Coverage ==="
while IFS= read -r -d '' file; do
    [[ -n "$file" ]] && remove_item "$file" "Go coverage file" || true
done < <(find . -type f \( -name "coverage.txt" -o -name "coverage.out" -o -name "coverage.html" \) -print0 2>/dev/null || true)

# 4. Go test binaries
echo ""
echo "=== Test Binaries ==="
while IFS= read -r -d '' file; do
    [[ -n "$file" ]] && remove_item "$file" "Test binary" || true
done < <(find . -type f -name "*.test" -print0 2>/dev/null || true)

# 5. Temporary session directories (from proxy tests)
echo ""
echo "=== Session Directories ==="
while IFS= read -r -d '' dir; do
    [[ -n "$dir" ]] && remove_item "$dir" "Test session directory" || true
done < <(find . -type d -name "sessions_*" -print0 2>/dev/null || true)

# 6. Test build artifacts
echo ""
echo "=== Build Artifacts ==="
remove_item "dist" "Distribution directory" || true
remove_item "build" "Build directory" || true

# 7. Log files
echo ""
echo "=== Log Files ==="
while IFS= read -r -d '' file; do
    [[ -n "$file" ]] && remove_item "$file" "Test log file" || true
done < <(find . -type f -name "*.log" -path "*/test/*" -print0 2>/dev/null || true)

# 8. Node modules in test directories (optional - usually keep these)
echo ""
echo "=== Test Cache Directories ==="
remove_item ".cache" "Cache directory" || true
remove_item "node_modules/.cache" "Node cache" || true

# 9. Any .session_key files created by tests in repo root or temp dirs
echo ""
echo "=== Stray Session Keys ==="
while IFS= read -r -d '' file; do
    [[ -n "$file" ]] && remove_item "$file" "Stray session key" || true
done < <(find . -maxdepth 3 -type f -name ".session_key" ! -path "*/node_modules/*" ! -path "*/.git/*" -print0 2>/dev/null || true)

# 10. Files of exactly 44 bytes in pkg/proxy/ (test artifacts)
echo ""
echo "=== 44-byte Test Artifacts ==="
if [[ -d "pkg/proxy" ]]; then
    while IFS= read -r -d '' file; do
        [[ -n "$file" ]] && remove_item "$file" "44-byte test artifact" || true
    done < <(find pkg/proxy -type f -size 44c -print0 2>/dev/null || true)
fi || true

# Summary
echo ""
echo "==========================================="
if [[ "$DRY_RUN" == true ]]; then
    echo "DRY RUN COMPLETE"
    echo "Run without --dry-run to actually remove files"
else
    echo "CLEANUP COMPLETE"
fi
echo "Files/Directories processed: $((FILES_CLEANED + DIRS_CLEANED))"
echo "  - Files: $FILES_CLEANED"
echo "  - Directories: $DIRS_CLEANED"
echo "==========================================="
