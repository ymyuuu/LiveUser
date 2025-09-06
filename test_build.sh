#!/bin/bash

echo "测试编译LiveUser 2.0..."

# 检查go是否可用
if ! command -v go &> /dev/null; then
    echo "错误: 未找到go命令"
    exit 1
fi

# 清理旧的测试文件
rm -f liveuser-test

echo "开始编译..."
go build -o liveuser-test main_new.go

if [ $? -eq 0 ]; then
    echo "✅ 编译成功!"
    echo "📄 输出文件: liveuser-test"
    ls -la liveuser-test
    echo ""
    echo "运行测试:"
    ./liveuser-test --version
else
    echo "❌ 编译失败"
    exit 1
fi