package repository

import "context"

// ObjectStore 抽象对象存储，用于存放文档原始文件。
//
// 原始文件必须留存，原因有两个：
//   - 重新解析需要它（换解析器、换档位、修 bug 后重跑都要拿原文件）；
//   - 切片结果里的引用溯源需要能回到原图与原始版式。
type ObjectStore interface {
	// Put 写入对象。key 由调用方生成，同一份文档应使用稳定的 key 以便覆盖重传。
	Put(ctx context.Context, key string, content []byte, contentType string) error
	// Get 读取对象内容；对象不存在时返回 ErrNotFound。
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete 删除对象；对象不存在时不视为错误（删除是幂等的）。
	Delete(ctx context.Context, key string) error
}
