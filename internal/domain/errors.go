package domain

import "fmt"

// 稳定错误码：前端按 Code 决定 UI，不依赖 Message 文案。
const (
	CodeUnknown            = "UNKNOWN"
	CodePathNotFound       = "PATH_NOT_FOUND"
	CodeToolMissing        = "TOOL_MISSING"
	CodeJDKMissing         = "JDK_MISSING"
	CodeJDKTooOld          = "JDK_TOO_OLD"
	CodePermissionDenied   = "PERMISSION_DENIED"
	CodeDiskFull           = "DISK_FULL"
	CodeMirrorUnreachable  = "MIRROR_UNREACHABLE"
	CodeMirrorIndexOnly    = "MIRROR_INDEX_ONLY"
	CodeChecksumMismatch   = "CHECKSUM_MISMATCH"
	CodeDownloadFailed     = "DOWNLOAD_FAILED"
	CodeArchiveFailed      = "ARCHIVE_FAILED"
	CodeFileInUse          = "FILE_IN_USE"
	CodeAccelUnavailable   = "ACCEL_UNAVAILABLE"
	CodePortExhausted      = "PORT_EXHAUSTED"
	CodeAvdNameInvalid     = "AVD_NAME_INVALID"
	CodeAvdExists          = "AVD_EXISTS"
	CodeAvdNotFound        = "AVD_NOT_FOUND"
	CodeImageNotInstalled  = "IMAGE_NOT_INSTALLED"
	CodeJobCanceled        = "JOB_CANCELED"
	CodeJobBusy            = "JOB_BUSY"
	CodeNotImplemented     = "NOT_IMPLEMENTED"
	CodeProcessFailed      = "PROCESS_FAILED"
	CodeInvalidArgument    = "INVALID_ARGUMENT"
	CodeLicenseNotAccepted = "LICENSE_NOT_ACCEPTED"
)

// AppError 是跨 Wails 边界的统一错误。
type AppError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Hint    string   `json:"hint,omitempty"`
	Detail  string   `json:"detail,omitempty"`
	Actions []Action `json:"actions,omitempty"`
}

// Action 是错误附带的可执行动作（前端渲染为按钮）。
type Action struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Payload string `json:"payload,omitempty"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Hint != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Hint)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Err 构造一个 AppError。
func Err(code, message string) *AppError { return &AppError{Code: code, Message: message} }

// ErrDetail 构造带原始输出的 AppError。
func ErrDetail(code, message, detail string) *AppError {
	return &AppError{Code: code, Message: message, Detail: detail}
}

// WithHint 追加建议。
func (e *AppError) WithHint(hint string) *AppError { e.Hint = hint; return e }

// WithDetail 追加上下文输出。
func (e *AppError) WithDetail(detail string) *AppError { e.Detail = detail; return e }

// WithAction 追加可执行动作。
func (e *AppError) WithAction(kind, label, payload string) *AppError {
	e.Actions = append(e.Actions, Action{Kind: kind, Label: label, Payload: payload})
	return e
}

// Wrap 把任意 error 包装为 AppError（nil 安全）。
func Wrap(code, message string, err error) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{Code: code, Message: message, Detail: err.Error()}
}

// IsNotImplemented 判断是否为占位实现返回的错误。
func IsNotImplemented(err error) bool {
	ae, ok := err.(*AppError)
	return ok && ae.Code == CodeNotImplemented
}

// NotImplemented 是骨架阶段的占位错误；实现后必须删除对应用法。
func NotImplemented(what string) *AppError {
	return Err(CodeNotImplemented, fmt.Sprintf("%s 尚未实现", what)).
		WithHint("这是框架骨架的占位实现")
}
