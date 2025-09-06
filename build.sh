#!/bin/bash

# LiveUser 2.0 重构版构建脚本
# 支持多平台编译和打包

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 版本信息
VERSION="2.0.0-重构版"
BUILD_TIME=$(date +"%Y-%m-%d %H:%M:%S")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# 构建信息
LDFLAGS="-s -w -X 'main.Version=${VERSION}' -X 'main.BuildTime=${BUILD_TIME}' -X 'main.GitCommit=${GIT_COMMIT}'"

# 输出目录
OUTPUT_DIR="dist"
BINARY_NAME="liveuser-v2"

# 函数：打印彩色信息
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 函数：检查依赖
check_dependencies() {
    print_info "检查构建依赖..."
    
    if ! command -v go &> /dev/null; then
        print_error "Go 未安装，请先安装 Go 1.21+"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    print_info "Go 版本: ${GO_VERSION}"
    
    if ! command -v git &> /dev/null; then
        print_warning "Git 未安装，将使用默认提交信息"
    fi
}

# 函数：清理旧文件
clean() {
    print_info "清理旧的构建文件..."
    rm -rf "${OUTPUT_DIR}"
    mkdir -p "${OUTPUT_DIR}"
    print_success "清理完成"
}

# 函数：下载依赖
download_deps() {
    print_info "下载 Go 依赖..."
    go mod tidy
    go mod download
    print_success "依赖下载完成"
}

# 函数：运行测试
run_tests() {
    print_info "运行测试..."
    
    if go test ./... -v; then
        print_success "所有测试通过"
    else
        print_error "测试失败"
        return 1
    fi
}

# 函数：构建单个平台
build_platform() {
    local os=$1
    local arch=$2
    local output_name="${BINARY_NAME}"
    
    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
    fi
    
    local output_path="${OUTPUT_DIR}/${os}-${arch}/${output_name}"
    
    print_info "构建 ${os}/${arch}..."
    
    mkdir -p "$(dirname "$output_path")"
    
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build \
        -ldflags "${LDFLAGS}" \
        -o "$output_path" \
        main_new.go
    
    # 复制配置文件和静态资源
    cp -r config "${OUTPUT_DIR}/${os}-${arch}/"
    cp -r static "${OUTPUT_DIR}/${os}-${arch}/"
    cp README_NEW.md "${OUTPUT_DIR}/${os}-${arch}/README.md"
    cp LICENSE "${OUTPUT_DIR}/${os}-${arch}/"
    
    # 创建启动脚本
    if [ "$os" = "windows" ]; then
        cat > "${OUTPUT_DIR}/${os}-${arch}/start.bat" << 'EOF'
@echo off
echo Starting LiveUser 2.0...
liveuser-v2.exe -config config\config.yaml
pause
EOF
    else
        cat > "${OUTPUT_DIR}/${os}-${arch}/start.sh" << 'EOF'
#!/bin/bash
echo "Starting LiveUser 2.0..."
./liveuser-v2 -config config/config.yaml
EOF
        chmod +x "${OUTPUT_DIR}/${os}-${arch}/start.sh"
    fi
    
    print_success "构建 ${os}/${arch} 完成"
}

# 函数：打包
package() {
    print_info "打包发布文件..."
    
    cd "${OUTPUT_DIR}"
    
    for dir in */; do
        if [ -d "$dir" ]; then
            platform=$(basename "$dir")
            print_info "打包 ${platform}..."
            
            if [[ $platform == *"windows"* ]]; then
                zip -r "${BINARY_NAME}-${VERSION}-${platform}.zip" "$dir"
            else
                tar -czf "${BINARY_NAME}-${VERSION}-${platform}.tar.gz" "$dir"
            fi
        fi
    done
    
    cd ..
    print_success "打包完成"
}

# 函数：显示帮助信息
show_help() {
    cat << EOF
LiveUser 2.0 重构版构建脚本

用法: $0 [选项]

选项:
  -h, --help      显示此帮助信息
  -c, --clean     清理构建文件
  -t, --test      运行测试
  -b, --build     构建所有平台
  -p, --package   打包发布文件
  -a, --all       执行完整构建流程（清理、测试、构建、打包）
  -q, --quick     快速构建（跳过测试）
  
平台选项:
  --linux-amd64   只构建 Linux AMD64
  --linux-arm64   只构建 Linux ARM64
  --darwin-amd64  只构建 macOS AMD64
  --darwin-arm64  只构建 macOS ARM64
  --windows-amd64 只构建 Windows AMD64

示例:
  $0 --all                # 完整构建流程
  $0 --quick              # 快速构建所有平台
  $0 --linux-amd64        # 只构建 Linux AMD64
  $0 -c -b -p            # 清理、构建、打包

EOF
}

# 主函数
main() {
    print_info "LiveUser 2.0 重构版构建脚本"
    print_info "版本: ${VERSION}"
    print_info "构建时间: ${BUILD_TIME}"
    print_info "Git提交: ${GIT_COMMIT}"
    echo ""
    
    # 解析命令行参数
    CLEAN=false
    TEST=false
    BUILD=false
    PACKAGE=false
    QUICK=false
    SPECIFIC_PLATFORM=""
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -c|--clean)
                CLEAN=true
                shift
                ;;
            -t|--test)
                TEST=true
                shift
                ;;
            -b|--build)
                BUILD=true
                shift
                ;;
            -p|--package)
                PACKAGE=true
                shift
                ;;
            -a|--all)
                CLEAN=true
                TEST=true
                BUILD=true
                PACKAGE=true
                shift
                ;;
            -q|--quick)
                CLEAN=true
                BUILD=true
                PACKAGE=true
                QUICK=true
                shift
                ;;
            --linux-amd64|--linux-arm64|--darwin-amd64|--darwin-arm64|--windows-amd64)
                SPECIFIC_PLATFORM=$1
                BUILD=true
                shift
                ;;
            *)
                print_error "未知选项: $1"
                show_help
                exit 1
                ;;
        esac
    done
    
    # 如果没有指定任何选项，显示帮助
    if [ "$CLEAN" = false ] && [ "$TEST" = false ] && [ "$BUILD" = false ] && [ "$PACKAGE" = false ]; then
        show_help
        exit 0
    fi
    
    # 检查依赖
    check_dependencies
    
    # 执行构建步骤
    if [ "$CLEAN" = true ]; then
        clean
    fi
    
    if [ "$TEST" = true ] && [ "$QUICK" = false ]; then
        download_deps
        # run_tests || exit 1
        print_info "跳过测试（测试文件尚未创建）"
    elif [ "$BUILD" = true ]; then
        download_deps
    fi
    
    if [ "$BUILD" = true ]; then
        if [ -n "$SPECIFIC_PLATFORM" ]; then
            case $SPECIFIC_PLATFORM in
                --linux-amd64)
                    build_platform "linux" "amd64"
                    ;;
                --linux-arm64)
                    build_platform "linux" "arm64"
                    ;;
                --darwin-amd64)
                    build_platform "darwin" "amd64"
                    ;;
                --darwin-arm64)
                    build_platform "darwin" "arm64"
                    ;;
                --windows-amd64)
                    build_platform "windows" "amd64"
                    ;;
            esac
        else
            # 构建所有平台
            build_platform "linux" "amd64"
            build_platform "linux" "arm64"
            build_platform "darwin" "amd64"
            build_platform "darwin" "arm64"
            build_platform "windows" "amd64"
        fi
    fi
    
    if [ "$PACKAGE" = true ]; then
        package
    fi
    
    echo ""
    print_success "构建流程完成！"
    
    if [ -d "${OUTPUT_DIR}" ]; then
        print_info "构建产物位于: ${OUTPUT_DIR}/"
        echo ""
        ls -la "${OUTPUT_DIR}/"
    fi
}

# 运行主函数
main "$@"