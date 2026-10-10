// 后端服务地址（仅服务端可见，不会暴露给浏览器）。
// 开发调试、隧道演示时由 Next 反向代理到后端，实现同源访问。
const BACKEND_ORIGIN = process.env.BACKEND_ORIGIN ?? "http://127.0.0.1:8888";

/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    // 统一走同源：前端请求发到当前站点，由这里转发给后端。
    // 好处：不产生跨域、也不会把「只在开发机有效」的 127.0.0.1 烧进前端产物。
    // 生产环境用 Nginx 按路径直接转发（Next 只输出静态文件，这些 rewrites 不会生效）。
    return [
      { source: "/healthz", destination: `${BACKEND_ORIGIN}/healthz` },
      { source: "/readyz", destination: `${BACKEND_ORIGIN}/readyz` },
      { source: "/api/:path*", destination: `${BACKEND_ORIGIN}/api/:path*` },
    ];
  },
};

export default nextConfig;
