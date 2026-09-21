package domain

// NonNil 把 nil 切片换成空切片，保证 JSON 序列化结果是 [] 而不是 null。
//
// Go 的 nil 切片默认会序列化成 null，而前端常见的 `arr.length` / `arr.map(...)`
// 会因此直接抛异常并卸载整棵 React 树（表现为整窗白屏）。
// 因此所有对外返回数组的绑定方法（含结构体里的数组字段）都要过一遍这里的归一化。
func NonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
