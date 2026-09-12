import { useEffect, useRef, useState } from "react";

// useVisible defers work until the element scrolls near the viewport (a 200px head start), so a page
// with many heavy embeds initializes only the visible ones — off-screen content costs no JS, decode,
// or network until reached. Without IntersectionObserver (older engines, jsdom) everything counts as
// visible. Once visible it stays visible: scrolling away must not tear down a rendered diagram.
export function useVisible<T extends HTMLElement = HTMLDivElement>() {
  const ref = useRef<T | null>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) {
      return;
    }
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true);
          io.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);
  return { ref, visible };
}
