#!/usr/bin/env bash
# doctor.sh — track-plaud の前提チェック (experimental)
#
# 「使えるものの一覧」を返す。agent はこれを見て実行時確認の選択肢を作る。
# チェック項目: node >= 20 / plaud CLI / plaud 認証 / yap / curl / jq
#
# 出力は人間向けのプレーンテキスト。JSON 化は Open question（未確定）。

set -u

ok()   { printf '  [ok]   %s\n' "$1"; }
warn() { printf '  [warn] %s\n' "$1"; }
miss() { printf '  [miss] %s\n' "$1"; }

echo "track-plaud doctor (experimental) — 前提チェック"

# --- node >= 20 ----------------------------------------------------------
if command -v node >/dev/null 2>&1; then
  major=$(node --version | sed -E 's/^v([0-9]+).*/\1/')
  if [ "$major" -ge 20 ] 2>/dev/null; then
    ok "node $(node --version)"
  else
    warn "node $(node --version) は 20 未満"
  fi
else
  miss "node がありません"
fi

# --- plaud CLI ------------------------------------------------------------
if command -v plaud >/dev/null 2>&1; then
  ok "plaud $(plaud --version 2>/dev/null || echo '(version unknown)')"
else
  miss "plaud CLI がありません → npm install -g @plaud-ai/cli"
fi

# --- plaud 認証 -----------------------------------------------------------
if command -v plaud >/dev/null 2>&1; then
  # 認証状態の確認は plaud の出力に依存するため、簡易に files を試す。
  if plaud files --json >/dev/null 2>&1; then
    ok "plaud 認証済み"
  else
    warn "plaud 未認証または取得失敗 → plaud login"
  fi
fi

# --- yap -------------------------------------------------------------------
if command -v yap >/dev/null 2>&1; then
  ok "yap $(yap --version 2>/dev/null || echo '(version unknown)')"
else
  miss "yap がありません → brew install finnvoor/tools/yap (macOS 26+ 必須)"
fi

# --- curl / jq -------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then ok "curl"; else miss "curl がありません"; fi
if command -v jq  >/dev/null 2>&1; then ok "jq";  else miss "jq がありません"; fi

echo
echo "まとめ: 上記 [ok] のツールが選択肢になります。[miss] は導線に従って導入してください。"
