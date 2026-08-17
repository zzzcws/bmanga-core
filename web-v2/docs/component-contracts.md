# V2 展示组件合同

本文记录 `src/components` 中稳定展示组件的职责边界。这里的“合同”是重构时必须保持的行为与 DOM 约定；视觉细节仍由 `styles.css` 决定。

## 通用边界

- 组件只接收展示所需数据并通过回调上报意图，不直接请求 API、不读写阅读进度，也不拥有路由或业务状态。
- `CatalogItem` 的兼容字段、标题清洗、页数和进度推导统一由 `lib/catalogPresentation.ts` 处理，不在组件内复制另一套规则。
- 现有 class 名和关键 DOM 层级属于样式合同。修改它们时必须同步检查 320、390、768、1180、1440、1920 六档视口。
- 页码、索引等业务数据在组件 props 中按下文约定传递；可见文案是否加一由组件合同决定，不由调用方预处理。

## `Brand`

**职责：** 输出全站一致的品牌标识。

- 无 props、无交互、无业务状态。
- 根节点保留 `.brand` 与 `aria-label="bmanga 私人漫画馆"`。
- 图形标记 `.brand-mark` 是装饰内容，必须保持 `aria-hidden="true"`；品牌名称和英文副标题仍需作为可读文本存在。

## `Cover` 与 `BookCard`

### `Cover`

| prop | 约定 |
| --- | --- |
| `item` | 原始 `CatalogItem`，封面 ID、标题和进度通过 catalog presentation helper 解析。 |
| `size` | 默认 `640`。`640` 表示列表卡片响应式封面合同，而不只是一个任意尺寸。 |
| `eager` | 默认 `false`；为 `true` 时同时使用 `loading="eager"` 与 `fetchPriority="high"`。 |
| `decorative` | 默认 `false`；为 `true` 时图片 `alt` 必须为空，否则为“{作品标题}封面”。 |

不可破坏合同：

- `size === 640` 时，`src` 请求 420 宽图片，并提供 `420 1x, 640 2x` 的 `srcSet`；其他尺寸请求传入尺寸且不生成该 `srcSet`。
- 图片加载失败只添加 `.image-failed`，封面占位 `.cover-placeholder` 始终保留。
- 阅读进度存在时，以 `.cover-progress` 宽度显示已读百分比。
- `Cover` 本身不响应点击；点击语义由外层卡片承担。

### `BookCard`

| prop | 约定 |
| --- | --- |
| `item` | 原始 `CatalogItem`。 |
| `index` | **零基索引**；组件负责显示为一基、至少两位的序号。 |
| `onOpen` | 点击根按钮时回传同一个 `item`。 |
| `context` | `default \| new \| search \| discover`，默认 `default`，只影响辅助文案。 |
| `priority` | 传给内部 `Cover.eager`，用于首屏优先加载。 |

- 根节点必须保持原生 `button[type="button"].book-card`，不能在其外再包点击容器。
- 卡片内的 `Cover` 必须是装饰图片，避免按钮可访问名称重复朗读封面标题。
- `.book-cover` 后接 `.book-info` 的结构、`.book-title-row` 内标题与序号的顺序属于现有 CSS 合同。
- `data-kind` 继续区分 `series`、候选类型或普通 `work`。

## `Status` 与 `CatalogSkeleton`

### `Status`

- `kind` 为 `loading \| error \| empty`，默认 `loading`；`children` 是可直接朗读的字符串。
- 错误使用 `role="alert"`，其他状态使用 `role="status"`，以便异步结果被辅助技术感知。

### `CatalogSkeleton`

- `count` 默认 12；`compact` 只切换 `.compact-grid`；`className` 追加而不替换基础 class。
- 整个可视骨架必须保持 `aria-hidden="true"`；组件同时输出一个独立的 `.sr-only[role="status"]`，用于稳定播报“正在加载作品”。
- 单个骨架继续输出一个封面块、一个常规文本条和一个短文本条，避免布局占位与真实卡片偏差过大。

## `Pagination`

| prop | 约定 |
| --- | --- |
| `page` | **一基页码**。非法或越界值在组件内钳制。 |
| `pages` | 总页数；非法值按 1 页处理，最小为 1。 |
| `label` | 分页 `nav` 的可访问名称。 |
| `kicker` | 可选装饰性短标签，不参与可访问名称。 |
| `onPageChange` | 只在钳制后的目标页与当前安全页不同时调用，回传一基页码。 |

- 首页、上一页、数字页、下一页、末页和跳页输入均使用同一套钳制规则。
- `page` 变化时同步跳页草稿；提交非数字内容时恢复当前安全页，不触发回调。
- 当前数字页保留 `aria-current="page"`；摘要保留 `aria-live="polite"`。
- `.pagination-main` 的直系节点顺序是硬合同：`首页按钮 → 上一页按钮 → .pagination-pages → .pagination-summary → 下一页按钮 → 末页按钮`。移动按钮、增加直系按钮或套额外容器前，必须同步修改移动端使用的 `:nth-of-type` / `:nth-last-of-type` 规则。
- 分页切换由父视图负责数据加载、滚动及焦点恢复；本组件不假定请求成功。

## `SectionHeader`

- `title` 始终输出为 `h2`；`eyebrow` 是同一标题块内的可选补充文本。
- 只有 `action` 和 `onAction` 同时存在时才渲染 `.text-button`，点击只调用 `onAction`。
- 保持 `header.section-head > div` 的标题分组结构，箭头为装饰内容并继续 `aria-hidden="true"`。

## `ReaderTopbar`、`ReaderControls` 与 `ReaderProgress`

阅读器组件只负责呈现和上报意图。页面预取、键盘监听、阅读进度队列、结尾页判断、跨作品切换和校准状态都属于 `App` 中的阅读器控制器。

### `ReaderTopbar`

- `currentIndex` 与 `requestedIndex` 均为**零基索引**；组件负责显示为一基页码。
- 加载目标页且目标索引不同于当前索引时，页码显示“当前 → 目标”；否则显示“当前 / 总页数”。
- `liveStatus` 是父控制器生成的完整读屏播报；组件用独立 polite live region 呈现，不自行推断保存结果。
- `inactive` 为真时顶栏必须同时 `inert` 与 `aria-hidden`，把焦点和语境交给校准弹层。
- 组件必须继续使用 `forwardRef<HTMLButtonElement>`，ref 指向“退出阅读”按钮，供打开/关闭阅读器时恢复或移动焦点。
- `onClose` 只表达退出意图，`onReveal` 只表达显示阅读器 chrome 的意图；组件不自行导航或计时。
- 顶栏根节点、关闭按钮、标题块、键盘提示、页数的顺序属于响应式样式合同。

### `ReaderControls`

- `fitMode` 接受 `fit-page \| fit-width \| split-wide`；按钮用 `aria-pressed` 表示当前模式。`split-wide` 只是用户偏好，是否真的拆分由父控制器结合图片自然宽高判断。
- `pageDraft` 是父控制器持有的受控字符串。输入时只把去除非数字字符后的值交给 `onPageDraftChange`；表单提交调用 `onPageDraftCommit`，组件不自行换算索引。
- `requestedIndex` 是零基索引，用于前后页禁用和下一步文案；`pageCount` 是总页数。
- `onFirst` / `onLast` 是物理首页和末页意图；当同一横页位于左半页时，组件先用 `onPrevious` 回到右半页，不能因物理索引相同而禁用。
- `splitWideActive` 与 `splitPanel` 只控制“右半页 / 左半页”文案和边界按钮；实际图片裁切、顺序、保存与完成判断仍由父控制器负责。
- `onOpenNextItem` 与 `nextItemLabel` 是可选次级操作。长下一话标题必须可省略，组件不自行加载目录之外的数据。
- `calibrationOpen` 时禁用翻页和页码输入。`ending`、`hasNextItem`、`imageLoading` 共同决定末页按钮文案和禁用状态，但实际导航仍由父控制器执行。
- `pendingProgressCount` 只显示“待同步 N”或“进度已同步”，组件不操作同步队列。
- `inactive` 为真时控制栏必须同时 `inert` 与 `aria-hidden`，不能与校准弹层争夺焦点。
- `.reader-controls` 的第一个直系按钮必须是“上一页”，最后一个直系按钮必须是“下一页/下一话”。移动端 CSS 依赖 `> button:first-child` 与 `> button:last-child` 安排网格区域。

### `ReaderProgress`

- `currentIndex` 是零基索引，进度计算为 `(currentIndex + 1) / pageCount`；`pageCount <= 0` 时显示 0%。
- 组件只输出视觉进度条，不写入阅读进度，也不替代阅读器的可访问页码播报。

## 修改检查清单

1. 先确认改动仍属于展示层；涉及请求、路由、阅读状态或持久化时应留在父控制器或 `lib`。
2. 保持上述一基/零基约定，避免调用方和组件重复加一。
3. 修改封面加载属性时检查首屏网络优先级与 1x/2x URL。
4. 修改分页或阅读控制 DOM 时检查对应 `nth-of-type`、`first-child`、`last-child` 样式。
5. 运行 `npm test`、`npm run build`，并在六档视口验证无横向溢出、焦点可见和 44px 触控目标。
