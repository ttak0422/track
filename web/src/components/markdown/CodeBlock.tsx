import { Fragment, useEffect, useRef, useState } from "react";
import { copyText } from "./clipboard";
import { tokenizeCode } from "./highlight";

interface CodeBlockProps {
  lang: string;
  text: string;
}

// CodeBlock renders a fenced code block with a copy-to-clipboard button. The button briefly switches
// to a "Copied" state so the action is acknowledged, then resets.
export function CodeBlock({ lang, text }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    return () => {
      if (resetTimer.current !== undefined) {
        window.clearTimeout(resetTimer.current);
      }
    };
  }, []);

  async function copy() {
    const ok = await copyText(text);
    if (!ok) return;
    setCopied(true);
    if (resetTimer.current !== undefined) {
      window.clearTimeout(resetTimer.current);
    }
    resetTimer.current = window.setTimeout(() => setCopied(false), 1500);
  }

  return (
    <div className="code-block" data-language={lang || undefined}>
      <button
        type="button"
        className="code-copy"
        onClick={copy}
        aria-label={copied ? "Copied" : "Copy code"}
      >
        {copied ? "Copied" : "Copy"}
      </button>
      <pre className="code-block-pre">
        <code>{highlightCode(text, lang)}</code>
      </pre>
    </div>
  );
}

function highlightCode(text: string, lang: string) {
  const tokens = tokenizeCode(text, lang);
  return tokens.map((token, index) =>
    token.className ? (
      <span className={token.className} key={index}>
        {token.text}
      </span>
    ) : (
      <Fragment key={index}>{token.text}</Fragment>
    ),
  );
}
