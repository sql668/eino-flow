// 后端服务地址（仅服务端可见，不会暴露给浏览器）。
// 开发调试、隧道演示时由 Next 反向代理到后端，实现同源访问。
const BACKEND_ORIGIN = process.env.BACKEND_ORIGIN ?? "http://127.0.0.1:8888";

/** @type {import('next').NextConfig} */
const nextConfig = {
  // 关掉 Next 自身的响应压缩。
  //
  // 这不只是「优化」：开启压缩时，Next 对**代理转发**的响应会先压缩再发送，
  // 于是流式响应（SSE）会被攒成一大块再吐出来——实测对话的 229 条思考增量
  // 全部在 11.5 秒时才一起到达，前端的流式与打字机完全失效。
  // 关掉压缩后同一条请求的首字延迟回到 2.3 秒，增量逐条到达。
  compress: false,
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
