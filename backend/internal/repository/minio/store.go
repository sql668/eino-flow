// Package minio 提供基于 MinIO（S3 兼容协议）的对象存储实现。
//
// 用 S3 兼容协议而不是 MinIO 私有接口，是为了将来能平滑切到云厂商对象存储
// （腾讯云 COS、阿里云 OSS 都兼容 S3），只换 endpoint 与凭证即可。
package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"einoflow-backend/internal/repository"
)

// Options 是构造参数，由上层从配置注入。
type Options struct {
	Endpoint  string // 形如 127.0.0.1:9000
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

// Store 实现 repository.ObjectStore。
type Store struct {
	client *minio.Client
	bucket string
}

// New 构造存储，并确保 bucket 存在。
//
// bucket 在启动时创建而不是依赖人工初始化：少一步部署操作就少一个出错点。
// 多实例并发启动时 MakeBucket 可能撞车，因此先探测再创建，并容忍「已存在」错误。
func New(ctx context.Context, opts Options) (*Store, error) {
	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: opts.UseSSL,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 MinIO 客户端失败: %w", err)
	}

	store := &Store{client: client, bucket: opts.Bucket}
	if err := store.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

// ensureBucket 确保 bucket 存在。
func (s *Store) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("检查 bucket %s 失败: %w", s.bucket, err)
	}
	if exists {
		return nil
	}

	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		// 并发启动时另一个实例可能刚好创建成功，这种情况不算失败。
		response := minio.ToErrorResponse(err)
		switch response.Code {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return nil
		}
		return fmt.Errorf("创建 bucket %s 失败: %w", s.bucket, err)
	}
	return nil
}

// Put 写入对象。
func (s *Store) Put(ctx context.Context, key string, content []byte, contentType string) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, s.bucket, key,
		bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("写入对象 %s 失败: %w", key, err)
	}
	return nil
}

// Get 读取对象内容。
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取对象 %s 失败: %w", key, err)
	}
	defer func() { _ = object.Close() }()

	content, err := io.ReadAll(object)
	if err != nil {
		if isNotFound(err) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("读取对象 %s 内容失败: %w", key, err)
	}
	return content, nil
}

// Delete 删除对象。对象不存在时返回 nil——删除操作按幂等处理，
// 调用方不需要先探测存在性。
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("删除对象 %s 失败: %w", key, err)
	}
	return nil
}

// Ping 探测对象存储连通性，供就绪检查使用。
func (s *Store) Ping(ctx context.Context) error {
	if _, err := s.client.BucketExists(ctx, s.bucket); err != nil {
		return fmt.Errorf("对象存储不可用: %w", err)
	}
	return nil
}

// isNotFound 判断 MinIO 返回的错误是否为「对象不存在」。
func isNotFound(err error) bool {
	response := minio.ToErrorResponse(err)
	return response.Code == "NoSuchKey" || response.StatusCode == http.StatusNotFound
}
