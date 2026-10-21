// 模型映射编辑器的行数据与 JSON 文本之间的转换规则。
//
// 编辑器的输入框是受控组件：每次按键都会把新的 JSON 通过 onChange 交给父级，
// 父级再以 value 传回。若把这份「自己刚发出的值」当作外部变更重新解析成行，
// 行 id 会被重新生成，React 按新 key 卸载重挂 <Input>，用户每输入一个字符就
// 丢一次焦点。因此这里把「是否属于自己发出的回声」单独建模，并让行 id 来自
// 单调计数器而不是时间戳。

export type ModelMappingRow = {
  id: string
  from: string
  to: string
}

let rowSequence = 0

export function nextModelMappingRowId(): string {
  rowSequence += 1
  return `model-mapping-${rowSequence}`
}

// parseModelMappingRows 把 JSON 文本解析成行。
// 空文本返回空数组；非法 JSON 返回 null，表示「保持现有行不变」。
export function parseModelMappingRows(
  json: string,
  makeId: () => string = nextModelMappingRowId
): ModelMappingRow[] | null {
  const trimmed = json.trim()
  if (!trimmed) {
    return []
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null
  }
  return Object.entries(parsed as Record<string, unknown>).map(
    ([from, to]) => ({
      id: makeId(),
      from,
      to: String(to),
    })
  )
}

// rowsToModelMappingJson 把行序列化回 JSON。from 为空的行不写进结果，
// 因为空键在映射里没有意义；全部为空时返回空串，与「未配置」一致。
export function rowsToModelMappingJson(rows: ModelMappingRow[]): string {
  const obj: Record<string, string> = {}
  for (const row of rows) {
    const from = row.from.trim()
    if (from) {
      obj[from] = row.to.trim()
    }
  }
  if (Object.keys(obj).length === 0) {
    return ''
  }
  return JSON.stringify(obj, null, 2)
}

// isOwnModelMappingEcho 判断传入的 value 是否就是本组件刚刚发给父级的那份。
// 是则不要重建行，否则每按键都会因行 id 变化而丢焦点。
export function isOwnModelMappingEcho(
  incoming: string,
  lastEmitted: string | null
): boolean {
  return lastEmitted !== null && incoming === lastEmitted
}
