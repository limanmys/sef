/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  output: "standalone",
  allowedDevOrigins: ["*", "liman.io", "*.liman.io"],
  devIndicators: false,
  rewrites: async () => {
    return [
      {
        source: "/api/:path*",
        destination: `http://backend:8110/api/:path*`,
      },
    ]
  },
  // Increase timeouts for long-running requests
  experimental: {
    proxyTimeout: 300000, // 5 minutes
  },
  // Headers for better streaming support
  async headers() {
    return [
      {
        source: '/api/:path*',
        headers: [
          {
            key: 'X-Accel-Buffering',
            value: 'no',
          },
          {
            key: 'Cache-Control',
            value: 'no-cache',
          },
        ],
      },
    ]
  },
}

export default nextConfig
