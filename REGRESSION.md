# 采集响应解析 / 筛选 / 改写回归说明

本目录新增了一套针对 ajson 现有接口的回归验证，覆盖「解析 → JSONPath 筛选 → 节点改写 → Marshal 回读」的完整链路。新增代码只使用 Go 标准库做验证，离线可运行，不改动任何生产实现，也不影响原有测试。

## 运行方式

```bash
# 全部测试（含 4 个保留的缺陷断言，会失败，见下文）
go test ./...

# 只看回归套件（不含缺陷断言，应全绿）
go test -run 'TestRegression' ./...

# 只看缺陷复现（预期失败）
go test -run 'TestDefect' -v .
```

## 样本与预期数据

- `testdata/samples/collection_response.json` — 采集响应主样本：嵌套对象、异构数组（`mixed`）、显式 `null`、带点号的键（`request.id`、`nested.key`）、带引号的键（`weird"key`、`quoted'key`）、转义字符（`\n \t \" \\`）、多字节文本（中文 / 日文 / 希腊文 / emoji）、超过 2^53 的大整数（`shard_id: 9007199254740993`）。记录以业务标识 `REC-001..005` 区分；`REC-004` 缺 `discount` 键、`note` 为空串，用于区分「字段缺失」与「字段为空」。
- `testdata/samples/invalid_*.json` — 失败路径样本：截断、非法转义（`\x`）、尾随垃圾，分别核对错误类型（`UnexpectedEOF` / `WrongSymbol`）。
- `testdata/expected/*.json` — 预期数据，按业务场景手工编写，**不是**用库的输出反推生成的。解析时用 `encoding/json` + `UseNumber` 解码，保证大整数字面量精确。

## 覆盖矩阵

| 场景 | 测试 |
| --- | --- |
| 解析：值 / 尺寸 / 缺失 vs 空值 / 大整数原值 | `regression_parse_test.go` |
| 解析失败：截断、非法转义、尾随垃圾（文件 + 内联） | `regression_parse_test.go` |
| 筛选：递归、条件过滤、切片、特殊键，按记录 ID 核对 | `regression_jsonpath_test.go` |
| 空结果不补空值节点；数组命中顺序 | `regression_jsonpath_test.go` |
| 冷读 vs 预读后同一查询结果一致 | `TestRegressionJSONPath_ColdVsPreRead` |
| 改写：替换 / 删除 / 复制 / 跨容器移动，同一响应连续处理 3 次 | `TestRegressionMutation_Stages` |
| 先读整树再改深层字段，祖先查询同步 | `TestRegressionMutation_AncestorQuerySync` |
| 数组删中间项后索引 / 父节点 / 后续筛选对应正确对象 | `checkStageDelete` |
| Clone 子树独立修改、原树不变；移动脱离原位置（非复制） | `checkStageCopy` / `checkStageMove` |
| Marshal 回读：重编码 → 重解析 → 重筛选，逐项比较（两轮） | `regression_marshal_test.go` |
| 缺陷最小复现（保留失败断言） | `regression_defects_test.go` |

改写目标一律通过 JSONPath 命中后再修改，保证改写落在实际命中的对象上；数值改写只使用可精确表示的值（79.5、2、150 等）。对象字段比较不强求顺序，数组严格按索引顺序比较。

## 已确认缺陷（保留失败断言，未改生产实现）

**容器值缓存失效缺失（stale container value cache）**

- 现象：容器（数组 / 对象）在调用过 `GetArray` / `GetObject` / `Value` 之后，再增删成员，缓存的旧成员列表会回流——`GetArray` / `GetObject` 返回改动前的内容，与 `Size()`、`GetIndex` / `GetKey`、`Unpack`、JSONPath、`Marshal` 的结果不一致。
- 根因：`node.go:getValue` 把容器成员缓存进 `Node.value`；`node_mutations.go` 中 `Node.update` 会重置该缓存，但成员变更路径 `Node.remove`、`Node.appendNode`、`Node.mark` 不会。
- 影响面：任何「读 → 改 → 再读」且通过 `Value()` / `GetArray` / `GetObject` 访问容器的流程会读到旧数据。`Marshal`、`Unpack`、JSONPath 筛选、`Size` 等基于 `children` 的访问不受影响。
- 最小复现：`regression_defects_test.go` 中 4 个 `TestDefect_*` 测试（DeleteIndex / AppendArray / DeleteKey / AppendObject 各一），断言表达的是期望行为，因此对当前实现失败。修复方向是在 `remove` / `appendNode`（或 `mark`）中同步失效 `Node.value`，本次未改动。

## 已核实的边界行为（非缺陷）

- 尾随垃圾（对象 / 数组 / 标量之后）均报 `WrongSymbol`，出错时返回 nil root。
- 未修改的大整数经 `Source()` 与整树 `Marshal` 保留原始字面量；`GetNumeric` 为 float64 近似（预期行为，故改写只用可精确表示范围）。
- `AppendObject` / `AppendArray` 挂载已有节点时会将其从原父节点摘除（移动语义），测试按移动而非复制核对。
- 过滤器中 `@.field == null` 只命中显式 null，不命中字段缺失的记录（`==` 对 nil 操作数返回 false，见 `math.go`）；字段缺失与空值的区分以 `HasKey` / `IsNull` 为准。
