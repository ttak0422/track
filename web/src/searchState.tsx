import { ReactNode, createContext, useContext, useMemo, useState } from "react";

interface SearchState {
  query: string;
  setQuery: (query: string) => void;
}

const SearchContext = createContext<SearchState | null>(null);

interface SearchProviderProps {
  children: ReactNode;
}

export function SearchProvider({ children }: SearchProviderProps) {
  const [query, setQuery] = useState("");
  const value = useMemo(() => ({ query, setQuery }), [query]);

  return <SearchContext.Provider value={value}>{children}</SearchContext.Provider>;
}

export function useSearchState(): SearchState {
  const value = useContext(SearchContext);
  if (!value) {
    throw new Error("useSearchState must be used inside SearchProvider");
  }
  return value;
}
