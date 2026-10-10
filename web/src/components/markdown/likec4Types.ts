export interface LikeC4View {
  id: string;
  title: string;
  svg: string;
}

export type LikeC4Result =
  | { status: "ready"; views: LikeC4View[] }
  | { status: "error"; message: string };

export const maxLikeC4SourceLength = 100_000;
export const maxLikeC4Views = 24;
export const maxLikeC4ViewNodes = 500;
