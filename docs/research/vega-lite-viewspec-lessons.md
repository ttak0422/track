# Vega-Lite の言語設計から track View Spec が学べること

Status: Research note(2026-07-07 調査)

track の View Spec(v2, ADR 0024)は Vega-Lite の mark × encoding 直交性を既に採用している。
本ノートは「表現力ギャップ」バックログ — 語彙追加が毎回 Go 編集になる構造をどこまで宣言的語彙の
成長で吸収し、どこから一般化機構(プラグイン/DSL)に切り替えるか — の判断材料として、Vega-Lite
の一次資料(公式ドキュメント[^docs]、論文[^paper]、スキーマ/リポジトリ)を track の制約
(**Go エンジンがデータを解決し、レンダラーは resolved series / pure JSON を受け取る。SVG レンダラーは
全マークを描く。strict validation で未知フィールドは拒否**)に照らして読んだ結果をまとめる。

[^docs]: Vega-Lite 公式ドキュメント: https://vega.github.io/vega-lite/docs/
[^paper]: A. Satyanarayan, D. Moritz, K. Wongsuphasawat, J. Heer, "Vega-Lite: A Grammar of Interactive Graphics", IEEE TVCG 2017. https://idl.cs.washington.edu/files/2017-VegaLite-InfoVis.pdf

要約(先に結論):

- Vega-Lite の強さは「小さく閉じた語彙 × 合成規則 × 導出されるデフォルト」であって、語彙の多さでは
  ない。track が採るべきは **順序付き `transform` 配列(closed union)** と **channel-local な `scale`
  オブジェクト** の 2 点。
- Vega-Lite が汎用性のために払っているコスト — expression 文字列、スキーマで表現できない
  per-mark validity、warning で落とす寛容さ — は track の strict validation 方針とは相容れない。
  **意図的に拒否**するのが正しい。
- interaction(selection/params)は Vega-Lite でもリアクティブランタイムへのコンパイルが前提で、
  ADR 0026 の pure-JSON option 制約下では成立しない。track の「interaction は形から機械的に導出」
  路線を維持する。

---

## 1. transform 語彙

### Vega-Lite の設計

Vega-Lite の `transform` はトップレベル(または view レベル)の**配列**で、aggregate / bin /
calculate / density / extent / filter / flatten / fold / impute / joinaggregate / loess / lookup /
pivot / quantile / regression / sample / stack / timeunit / window の約 20 種を持つ。実行順序は
明示仕様で、「**The transformations are executed in the order in which they are specified in the
array**」[^transform]。つまり `filter → window → aggregate` のような合成は配列の並びそのものが
セマンティクスになる。

[^transform]: https://vega.github.io/vega-lite/docs/transform.html

transform の置き場所は二つある: view レベルの `transform` 配列と、encoding のフィールド定義に
インラインで書く糖衣(`aggregate`, `bin`, `timeUnit`, `sort`, `stack`)[^encoding]。両方があるときの
展開順も固定されている — view レベルが先、インラインは「bin, timeUnit, aggregate, sort, and
stack」の固定順[^transform]。**インライン形は必ず配列形の固定展開として定義されている**ため、
糖衣を増やしても合成のセマンティクスは一箇所(配列)に留まる。

[^encoding]: https://vega.github.io/vega-lite/docs/encoding.html

track に直近で必要な `window`(移動平均)は Vega-Lite では
`{"window": [{"op": "mean", "field": ..., "as": ...}], "frame": [-k, 0], "sort": ..., "groupby": ...}`
という一 transform で、`frame` + 集約 op の組で移動平均を表す。公式ドキュメントが明記する罠が
二つ: 「**If sort is not specified, the order is undefined**」(sort なしの window は入力順依存)、
および `frame` が効くのは集約 op と `first_value`/`last_value`/`nth_value` だけで、rank 系 op は
`frame` を無視する[^window]。

[^window]: https://vega.github.io/vega-lite/docs/window.html

`filter` transform は (a) expression 文字列(`"datum.b2 > 60"`)、(b) field predicate オブジェクト
(`equal`/`lt`/`lte`/`gt`/`gte`/`range`/`oneOf`/`valid`)、(c) selection predicate、(d) それらの
and/or/not 合成、の 4 形を受ける[^filter]。track の現行 `filter`(eq/ne/lt/le/gt/ge の AND)は
(b) のサブセットに正確に対応する。

[^filter]: https://vega.github.io/vega-lite/docs/filter.html

`calculate` は Vega expression 文字列を評価する — つまり Vega-Lite は transform 語彙の中に
**式言語を丸ごと抱えている**。これが Vega-Lite の汎用性の源泉であると同時に、実装(式評価器)と
検証(文字列の中身はスキーマで検査できない)の両方で最も重いコンポーネントになっている。

### track への示唆

- **採用(今)**: `transform: []` 順序付き配列を bucket B(ADR 0024 の「データ変換」)の受け皿として
  導入する。ただし Vega-Lite と違い **closed union**(名前付き transform の固定集合、未知の
  transform 名・フィールドは error)にする。どうせ各 transform は Go 実装なので、閉じた集合は
  track の strict validation 方針と完全に整合する。最初の語彙は `window`(移動平均)と
  `filter`(既存 filter の移設 or 併存)だけでよい。「配列の並び = 実行順」の一文を仕様に置く
  ことが本体で、これで以後の transform 追加が「語彙 1 個 + Go 関数 1 個」に閉じる。
- **設計規則の輸入**: window には Vega-Lite に倣って `sort` を必須にする(track の JSONL は
  入力順が保証されないため、「sort なしは undefined」を仕様に書くより必須にして error にする方が
  track 流)。`groupby` は color split と同じ分割意味論を再利用する。
- **糖衣の位置づけ**: 予定されている「candlestick の `window` channel option」は Vega-Lite の
  インライン aggregate/bin と同型の糖衣。出すのは構わないが、**「transform 配列の固定展開である」
  と最初から定義しておく**こと。Vega-Lite の経験則は「糖衣は 1 op なら快適、2 op の合成
  (window + aggregate など)が要る瞬間に配列形が必要になる」であり、展開順を後決めすると
  互換性問題になる。
- **拒否**: `calculate` / filter expression 文字列。式言語の埋め込みは (1) Go に評価器を持ち込む、
  (2) strict validation の外側に検査不能な文字列領域を作る、(3) スペックが事実上プログラムになり
  「spec = what」の線が消える、の三重の罠。派生列が必要なら track では `track-fetch-*` /
  データ生成側の責務(ADR 0024 bucket B の元々の線)か、named transform(`calculate` ではなく
  例えば `ratio: {of, over}` のような意図名)として個別に足す。
- **罠(近づかない transform)**: `lookup`(第二データソースとの join — fetch 側でやる方が
  JSONL 的に自然)、`density`/`regression`/`loess`(統計ライブラリの重さに対して track の
  ユースケースが薄い。ただし結果はただの series なので、必要になれば engine 側追加は可能)、
  `fold`/`pivot`(wide↔long 変換。track は wide を `y[]`、long を `color` split で両方直接
  受けられるので、Vega-Lite が fold を必要とする理由の大半が track には存在しない)。

## 2. レイヤー合成(layer)

### Vega-Lite の設計

`layer` は view 合成代数の一演算子で、シグネチャは `layer([unit1, unit2, ...], resolve)`。重要なのは
**オペランドを unit view に制限している**ことで、論文は理由を明記する: 「**To prohibit layering of
composite views with incongruent internal structures, the layer operator restricts its operands to
be unit views**」(論文 §3.2.1)[^paper]。ドキュメント側でも「Specifications inside layer cannot
use row and column channels」— facet したいなら facet の内側に layer を入れる[^layer]。

[^layer]: https://vega.github.io/vega-lite/docs/layer.html

スケールはデフォルト共有で、「**When you have different scales in different layers, the scale
domains are unioned so that all layers can use the same scale**」[^layer]。二軸チャートは
`resolve: {"scale": {"y": "independent"}}` で表す(論文 Fig. 3(a) がまさに precipitation バー +
温度ラインの dual-axis 例)。親の `data`/`encoding` は各レイヤーに継承されるので、共通部分は
一度だけ書く。

そして Vega-Lite には **annotation 専用語彙が存在しない**。参照線は `rule` マークのレイヤー、
注釈テキストは `text` マークのレイヤー、帯は `rect` レイヤー — すべて mark + encoding の合成で
賄う[^layer]。

### track への示唆

track が個別語彙で持っているもの — `overlays`(markers / ref line / band / callout)と
`y[].mark`(combo)と `axis: "y2"` — は、Vega-Lite ではすべて layer + resolve の**導出形**である:

| track の語彙 | Vega-Lite での表現 |
|---|---|
| `overlays[].y`(ref line) | `rule` mark のレイヤー(literal data) |
| `overlays[].from/to`(band) | `rect` mark のレイヤー |
| `overlays[]` markers(第二ソース) | 別 data を持つ `rule` レイヤー |
| callout | `text` + `point` レイヤー |
| `y[].mark`(combo) | mark の違う 2 レイヤー |
| `axis: "y2"` | layer + `resolve: {scale: {y: "independent"}}` |

**flat overlay union が得ているもの**: 検証が有限(「4 形のうち exactly one」)、エンジンが再帰的な
spec 解決なしに resolve できる、SVG レンダラーの仕事が有限に留まる、そして overlay は「データの
上に乗る参照ジオメトリ」という編集意図が型に出る。**失っているもの**: 新しい注釈形が毎回 Go 語彙
(callout も box も各々 ADR になった)。Vega-Lite なら全部「既にある mark の合成」で語彙追加ゼロ。

- **採用(必要になったら)**: **制限付き layer**。もし overlay の第 5 形・第 6 形の要求が続く、
  あるいは candlestick + 移動平均線 + 出来高バーのような「複数 mark が一つの x を共有する」形が
  `y[].mark` の制約(line/bar/area のみ、color split と非併用)からはみ出すなら、
  `layers: [{mark, encoding, data?}]` + 「x は共有・y は resolve」だけの縮退版 layer が
  `y[].mark`、`axis:"y2"`、overlays の大半を一つの機構に畳める。その際 Vega-Lite の最重要の
  教訓は**オペランドの制限を強くかける**こと(unit のみ・ネスト不可・x 共有必須)。Vega-Lite が
  unit に絞ってもなお成立している事実が、「全部入り layer」は要らない証拠になっている。
- **ただし今は動かない方がよい**: track の overlay 4 形 + combo は現在の要求を全部満たしており、
  layer 導入は Resolve / 両レンダラー / article の全面改修になる。「overlay に新形を足す ADR が
  次に 2 回続いたら layer 化を検討する」程度のトリガーを置くのが妥当。
- **維持**: `y[]` の wide-data 多系列。Vega-Lite は wide データに repeat レイヤーか fold を
  要求し、repeat レイヤーには「**Even if different layers use different colors, Vega-Lite will not
  generate a legend and not stack marks**」という凡例・stack の欠落が公式に記されている[^repeat]。
  track の `y[]` はこの不便を最初から回避した設計であり、layer を入れる日が来ても `y[]` は
  糖衣として残す価値がある。

[^repeat]: https://vega.github.io/vega-lite/docs/repeat.html

## 3. マルチビュー(facet / concat / repeat)

### Vega-Lite の設計

論文 §3.2 の合成代数は layer / hconcat / vconcat / facet / repeat の 5 演算子で、「Each operator
is responsible for combining or aligning underlying scales and axes as needed」[^paper]。

- **facet**: `facet(channel, data, field, view, scale, axis, resolve)` — データをフィールド値で
  **分割**し、テンプレート view を各パーティションに適用する(trellis / small multiples)。
  デフォルトは「shared scales, axes, and legends」[^facet]。量的フィールドで facet するには
  bin が必要。
- **repeat**: facet と違い「**Unlike facet it allows full replication of a data set in each
  view**」— 分割ではなくフィールド名の代入で view を複製する(SPLOM 用)[^repeat]。
- **concat**: 任意 view の並置。デフォルトは「**independent scales and axes for position channels
  and shared scales and legends for all other channels**」で、しかも「**Currently, Vega-Lite does
  not support shared axes for concatenated views**」[^concat] — **並置した view の軸同期は
  Vega-Lite ですら未サポート**である(リンクは selection/params 側の仕事になる)。

[^facet]: https://vega.github.io/vega-lite/docs/facet.html
[^concat]: https://vega.github.io/vega-lite/docs/concat.html

### track への示唆

track の article/blocks は「resolve も interaction 共有もない vconcat」に相当する。文書としての
並置には十分だが、**ペイン間の同期(candlestick の出来高ペイン問題)には原理的に届かない** —
そして Vega-Lite の concat も届いていない、というのが今回の調査で最も直接的な発見になる。
Vega-Lite で価格 + 出来高の同期ペインを組むと vconcat + interval selection (`bind: "scales"`) の
共有という interaction 機構頼みになり、pure-JSON option 制約(ADR 0026)下の track には輸入
できない。

- **結論(candlestick 出来高ペイン)**: article レベルの合成(blocks/concat 方向)で解くのは
  誤り。**単一 spec 内**で解く — ECharts は multi-grid + 1 つの `dataZoom` が複数 `xAxisIndex` を
  張る構成を**純粋な JSON option だけで**表現できるので、「エンジンが 2 グリッドの option を emit
  する」のは ADR 0026 の線の内側に収まる。spec 語彙としては、出来高を `y[]` の
  `axis: "y2"`(同一ペイン)か、将来 `pane: 2` のような 1 knob(別ペイン)で表すのが最小。
  Vega-Lite が concat の軸共有を諦めていることが、「同期する物は一つの view の中に置く」という
  設計判断の先例になる。
- **採用(必要になったら)**: **facet channel(1 段)**。`encoding.facet: {field, columns?}` は
  Go 側では「records を値で分割して N 回 resolve」するだけで、Resolved の形を変えずに済む
  (Resolved の配列 + 共有スケール計算)。SVG レンダラーもグリッド配置の追加だけで描ける。
  Vega-Lite のデフォルト(scale 共有、ordinal だけ独立 — 論文 §3.2.3 の「空カテゴリを含めない」
  理由付き)はそのまま輸入してよい。
- **拒否**: repeat。track の `y[]` が「同一データ・複数フィールド」の主用途(多系列)を既に
  覆っており、SPLOM 需要は見えていない。concat の一般化も article/blocks が既に担う。

## 4. スケール/ガイド語彙(scale, axis, legend)

### Vega-Lite の設計

scale は channel のフィールド定義内の `scale` オブジェクトで、型は連続(linear, pow, sqrt, log,
symlog, time, utc)/離散(ordinal, band, point)/離散化(bin-ordinal, quantile, quantize,
threshold)の 3 分類[^scale]。**デフォルトは (channel, data type, mark) から推論される**ことが
明文化されている: 「For positional (x and y) nominal and ordinal fields, "band" scale is the
default scale type for bar, image, rect, and rule marks while "point" is the default scales for
all other marks」[^scale]。論文も「If not specified, Vega-Lite will automatically populate default
properties based on the channel and data-type」(§3.1)と、デフォルト導出を言語仕様の一部として
扱う。

[^scale]: https://vega.github.io/vega-lite/docs/scale.html

色は `scale.scheme` に**名前**(`"blues"`, `"redblue"` など)を渡す。名前空間は Vega の scheme
リファレンスに一元化されており、categorical / sequential(single-hue, multi-hue)/ **diverging** /
cyclical に分類される(diverging には blueorange, redblue, redyellowgreen, spectral 等)[^schemes]。
diverging スケールの中心合わせは scheme とは**別の直交 knob** `domainMid` で行う: 「Inserts a
single mid-point value into a two-element domain … useful for setting a midpoint for diverging
color scales」[^scale]。

[^schemes]: https://vega.github.io/vega/docs/schemes/#reference

default-vs-override の線引きは一貫している: **(1) すべての見た目に (channel, type, mark) 由来の
デフォルトがあり、(2) override はその channel の `scale`/`axis`/`legend` オブジェクトという
「効く場所」に置き、(3) 全チャート共通のテーマは spec ではなく `config` に置く**[^scale][^spec-doc]。

[^spec-doc]: https://vega.github.io/vega-lite/docs/spec.html

### track への示唆

track は現在 scale 語彙ゼロ(固定パレット、固定 ramp、軸自動)で、これは正しい出発点 —
Vega-Lite のデフォルト推論と同じことを「語彙なし」で達成している。treemap で `scale: "diverging"`
を足す予定に対して:

- **採用(今)**: **挙動名でなく scheme 名 + midpoint に分解する**。`scale: "diverging"` は
  「どの色か」と「どこが中心か」を 1 語に束ねてしまい、次の要求(中心が 0 でない、色を変えたい)で
  語彙が割れる。Vega-Lite に倣い color channel に
  `scale: {scheme?: "…", domainMid?: number}` を置く。ただし scheme の名前空間は Vega の
  数十個ではなく **closed list(2〜3 個: 既存の sequential ramp + diverging 1 種程度)** にする —
  track は echarts と svg の両レンダラーで色定数を共有しており(ADR 0026)、scheme 1 個の追加は
  両実装への追加を意味するから、リストが小さいことに実装上の必然がある。未知の scheme 名は
  strict validation で error。
- **placement 規則の輸入**: scale オプションは「効く channel の上」に置き、効かない場所では
  error にする — これは track が `sort`/`limit`/`stack` で既にやっている placement 検証
  (`validateChannelOptions`)の自然な延長で、Vega-Lite の置き場所設計と一致する。
- **採用(必要になったら)**: y channel の `scale: {type: "log"}`。価格系列では現実的な要求だが、
  SVG レンダラーの軸計算・目盛り生成に実装が要るので、要求が出るまで待つ。時間軸(temporal
  scale)は track が time を opaque category として扱う設計(visualization.md)を崩すので、
  導入するなら scale 語彙ではなくデータモデル側の決定として別途 ADR にすべき。
- **拒否**: axis/legend の外観語彙(grid, tickCount, labelAngle, …)。Vega-Lite ではこれらが
  語彙の物量の大半を占めるが、track では「見た目のデフォルトはレンダラー定数、テーマは config」
  という ADR 0028 と同じ what/how の線で、spec に入れない。

## 5. 直交性の保ち方

### Vega-Lite の設計

論文 §3.1 の定式化: `unit := (data, transforms, mark-type, encodings)`、
`encoding := (channel, field, data-type, value, functions, scale, guide)`[^paper]。mark の集合は
小さく閉じており、channel は mark と独立に一度だけ定義される — track の ADR 0024 が採った構造
そのもの。

ただし Vega-Lite も完全な直交ではない。**mark 固有 channel** が存在する: `x2`/`y2` は ranged
mark(`area`, `bar`, `rect`, `rule`)専用、`theta`/`radius` は `arc`(と `text`)専用、
`longitude`/`latitude` は地理投影 mark 専用[^encoding]。つまり「範囲」「極座標」「地理」という
座標系の違いは channel の追加として直交性の枠内に取り込み、mark ごとの適用可否は**言語の外**
(コンパイラ)で管理している。

その適用可否の扱いが Vega-Lite の弱点でもある: **JSON スキーマは per-mark validity を表現できず**、
不正な組み合わせはスキーマ検証を素通りしてコンパイル時 warning で **黙って落とされる**
(例: 「xOffset-encoding is dropped as xOffset is not a valid encoding channel」、また空の color
encoding がスキーマを通過する既知 issue[^issue4236])。スキーマは TypeScript ソースから生成され、
`$schema` プロパティのバージョン付き URL(`…/schema/vega-lite/v6.json`)で世代管理される —
破壊的変更はメジャーバージョン、**旧バージョンのスキーマ URL は生き続ける**ので古い spec は
古いスキーマに対して検証可能なまま残る[^spec-doc]。

[^issue4236]: https://github.com/vega/vega-lite/issues/4236 (Empty color encoding passes JSON schema validation)

型×機能マトリクスの爆発を Vega-Lite が回避している仕組みを分解すると:
(1) mark は幾何だけを名指し、意味は channel 側に置く、(2) 座標系の差(範囲・極・地理)は
**channel ファミリの追加**として吸収する、(3) 妥当性はスキーマでなくコンパイラの一箇所で検証する、
(4) 見た目の差は config/デフォルト導出に逃がす。失敗している(=直交が破れている)のは、
mark ごとの channel 解釈の分岐がコンパイラ内部に蓄積する点と、それがユーザーには warning と
してしか見えない点。

### track への示唆

- **維持(track の方が正しい)**: strict validation。Vega-Lite の「warning で落とす」は Web の
  寛容さの文化であり、track の「misplaced option は error」(`validateChannelOptions`、candlestick
  の form gate)は同じ問題への強い解。**per-mark validity はスキーマに書けない、バリデータに書く** —
  Vega-Lite の実態が track の現行実装を追認している。error メッセージが「どこに置くべきか」を
  言う track の流儀(`sortPlacementError`)はそのまま資産。
- **輸入(パターン)**: 「座標系の差は channel ファミリで吸収」。candlestick が今後
  y channels(移動平均線、出来高)を得るとき、Vega-Lite なら OHLC は `y`/`y2` ranged encoding +
  layer で組む形になる。track は「kind の canonical fields を mark が暗黙に読む」という別解を
  既に採っており(candlestick の OHLC 暗黙 encoding)、これは track のデータモデル(kind が
  スキーマを持つ)があって初めて成立する track 固有の直交性 — **kind × mark の結び付きは
  Vega-Lite にない武器**なので、treemap でも「kind の canonical fields を暗黙に読む」形を優先
  してよい。汎用フィールド指定が要る mark だけ channel を要求する。
- **輸入(バージョニング)**: `$schema` 的な「spec が自分の世代を名乗り、旧世代の検証器が
  参照可能で在り続ける」考え方。track の `version: 2` + 旧版 hard reject は未リリースの今は
  正しいが、**viewspec はノート本文(fenced block)や asset として vault に永続する**ため、
  ユーザーに配った瞬間から「古いノートの chart が数年後も描ける」ことが要件になる。v3 を
  切る日が来たら、Vega-Lite 式(旧スキーマ共存)か一方向マイグレータ(`track migrate`)の
  どちらかを ADR で決めておく必要がある。世代管理(`track gen`)を持つプロダクトとして、
  spec の世代も同じ真剣さで扱う、と先に書き残しておく価値がある。
- **拒否**: channel の物量拡大(opacity, strokeDash, shape, angle, …)。Vega-Lite の channel
  リストの長さは「あらゆるチャートを作れる」ための物であり、track の channel は「データ記事に
  要る表現 + 出典(detail/href/note)」に絞れている。出典 channel 群は Vega-Lite の
  `tooltip`/`href` に相当するがより深く(note 参照、publish 時 rewrite)、ここは track が
  Vega-Lite より進んでいる領域。

## 6. selection / params(短く)

Vega-Lite の interaction は params として宣言される: 単純な variable param と、
selection param(`point` / `interval`)。selection は論文 §4.1 で
`selection := (name, type, predicate, domain|range, event, init, transforms, resolve)` の 8-tuple と
定式化され、視覚レンジでなく**データドメイン上で**定義することで view を跨いだ再利用を可能にする
[^paper]。selection は filter transform・条件付き encoding・scale domain(`bind: "scales"` で
pan/zoom)に接続され、「Rather than writing event handlers, designers specify parameter
definitions once, then reference them throughout their specification」[^param]。

[^param]: https://vega.github.io/vega-lite/docs/parameter.html

ただしこの宣言性は**リアクティブランタイムへのコンパイルで実現される** — selection はイベント
ハンドラと Vega signal に落ち(論文 §4)、出力は「描画したら終わりの JSON」ではない。ADR 0026 の
pure-JSON option 制約(コールバックなし、`setOption` だけで成立)とは根本的に相容れない。

**track への示唆**: selection/params 語彙は**拒否**。track は既に「interaction は resolved form
から機械的に導出」(category-x なら zoom、30 カテゴリ超で slider、bar なら band shadow — 語彙
ゼロ)という線を引いており、これは Vega-Lite の「ambitious defaults」を interaction に適用した
形として筋が良い。輸入できる唯一のスライスは「**ECharts が config だけで実装している interaction
は、エンジンが導出するか、最小の editorial knob を与えてよい**」という判定基準:

- multi-grid を跨ぐ zoom 同期(`dataZoom.xAxisIndex: [0,1]`)— pure JSON。出来高ペインで使う(§3)。
- 初期 zoom 窓(長い candlestick で直近 N 本だけ見せる)— `dataZoom.startValue` は pure JSON。
  必要なら overlay ではなく view の knob(例: `zoom: {last: 90}`)1 語で足りる。
- click で強調・brush 連動・cross-filtering — コールバック/イベント必須。**frontend 所有**
  (EChartsBlock 側)に留め、spec 語彙にしない。ADR 0027/0028 の「エンジンが内容、frontend が
  ジオメトリ」の分割の interaction 版。

---

## 優先度付きショートリスト

| # | 施策 | 判定 | 一行根拠 |
|---|---|---|---|
| 1 | `transform: []` 順序付き配列(closed union、初期は `window` のみ。sort 必須・groupby は color split と同じ意味論) | **adopt now** | 「配列の並び = 実行順」を今決めれば、以後の bucket B 追加が語彙 1 個 + Go 関数 1 個に閉じる[^transform] |
| 2 | candlestick の window channel option は「transform 配列の糖衣」と定義してから出す | **adopt now** | Vega-Lite のインライン transform は固定展開順を持つ糖衣 — 後決めすると合成順で互換性が割れる[^transform] |
| 3 | color channel に `scale: {scheme, domainMid}`(scheme は closed list)— `scale: "diverging"` はこの分解形で出す | **adopt now** | 「どの色か」と「中心はどこか」は Vega-Lite でも直交 knob[^scale]。scheme 追加 = 両レンダラー実装なので closed list に必然性がある |
| 4 | 出来高ペインは article/concat でなく単一 spec 内の multi-grid(pure-JSON `dataZoom` 同期)で | **adopt now**(設計方針として) | Vega-Lite ですら concat の軸共有は未サポート[^concat] — 同期する物は一つの view に置くのが先例 |
| 5 | 制限付き `layers`(unit のみ・x 共有・y resolve)で overlays/combo を一般化 | **adopt when needed**(overlay の新形 ADR が 2 回続いたら) | Vega-Lite は annotation 語彙を持たず layer で賄う[^layer]が、track の flat union は今の要求を有限の検証で満たしている |
| 6 | `encoding.facet`(1 段の small multiples、scale 共有デフォルト) | **adopt when needed** | Go では「分割して N 回 resolve」で済み Resolved の形を変えない。デフォルトは Vega-Lite の facet 規則をそのまま輸入[^facet] |
| 7 | y channel の `scale: {type: "log"}` | **adopt when needed** | 価格系列で現実的だが SVG の軸実装コストがあるので要求駆動で |
| 8 | expression 文字列(calculate / filter expr)と selection/params 語彙 | **reject** | 式言語は strict validation の外に検査不能領域を作り、selection は pure-JSON option 制約(ADR 0026)と根本的に非互換[^param] |

---

## 参照一覧

- Vega-Lite 公式ドキュメント: [transform](https://vega.github.io/vega-lite/docs/transform.html) /
  [window](https://vega.github.io/vega-lite/docs/window.html) /
  [filter](https://vega.github.io/vega-lite/docs/filter.html) /
  [layer](https://vega.github.io/vega-lite/docs/layer.html) /
  [resolve](https://vega.github.io/vega-lite/docs/resolve.html) /
  [facet](https://vega.github.io/vega-lite/docs/facet.html) /
  [concat](https://vega.github.io/vega-lite/docs/concat.html) /
  [repeat](https://vega.github.io/vega-lite/docs/repeat.html) /
  [scale](https://vega.github.io/vega-lite/docs/scale.html) /
  [encoding](https://vega.github.io/vega-lite/docs/encoding.html) /
  [spec ($schema)](https://vega.github.io/vega-lite/docs/spec.html) /
  [parameter](https://vega.github.io/vega-lite/docs/parameter.html)
- Vega scheme reference: https://vega.github.io/vega/docs/schemes/#reference
- 論文: Satyanarayan, Moritz, Wongsuphasawat, Heer.
  [Vega-Lite: A Grammar of Interactive Graphics](https://idl.cs.washington.edu/files/2017-VegaLite-InfoVis.pdf).
  IEEE TVCG 2017.(§3.1 unit/encoding tuple、§3.2 合成代数と resolve、§3.2.3 facet の
  ordinal 独立スケール、§4.1 selection 8-tuple)
- スキーマの寛容さの実例: [vega-lite#4236](https://github.com/vega/vega-lite/issues/4236)
- track 側の前提: `internal/track/viewspec/viewspec.go`, `docs/adr/0024-mark-encoding-view-spec.md`,
  `docs/adr/0026-echarts-interactive-renderer.md`, `docs/adr/0028-marker-annotation-boxes.md`,
  `docs/spec/visualization.md`
