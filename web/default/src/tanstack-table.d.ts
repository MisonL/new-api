import '@tanstack/table-core'

// v8 的 ColumnMeta 是两个类型参数（TData, TValue）；v9 改成三个
// （TFeatures, TData, TValue）。合并声明必须跟上新的签名，否则列定义里的
// meta 字段会全部退化成空接口，取值时全报 "property does not exist"。
declare module '@tanstack/table-core' {
  interface ColumnMeta<_TFeatures, _TData, _TValue> {
    // Human-readable label for the column
    label?: string
    // Optional description shown in tooltips or help text
    description?: string
    // Whether this column can be sorted (overrides default behavior)
    sortable?: boolean
    // Custom CSS classes to apply to the column cells
    className?: string
    // Marks the primary row title in mobile card lists
    mobileTitle?: boolean
    // Places the cell beside the title in mobile card lists
    mobileBadge?: boolean
    // Omits the cell from mobile card lists
    mobileHidden?: boolean
  }
}
