# --- push-to-github.ps1 ---
# Run this from inside the folder you want to upload

# 1. Initialize repo if not already
if (!(Test-Path ".git")) {
    git init
}

# 2. Add and commit all files
git add .
git commit -m "Initial commit from Windows"

# 3. Remove any old remotes
git remote remove coder 2>$null
git remote remove trader 2>$null
git remote remove all 2>$null

# 4. Add remotes
git remote add coder  git@github.com-01:SpaceCadetOG/TraderBot.git
git remote add trader git@github.com-02:bicblockchainsolutions/TraderBot.git
git remote add all    git@github.com-01:SpaceCadetOG/TraderBot.git

git remote set-url --add --push all git@github.com-01:SpaceCadetOG/TraderBot.git
git remote set-url --add --push all git@github.com-02:bicblockchainsolutions/TraderBot.git
git config remote.pushDefault all

# 5. Push to GitHub
git branch -M main
git push origin main



git add -A && git commit -m "..." && git push origin main
