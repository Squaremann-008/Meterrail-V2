import type { NextConfig } from 'next';

/**
 * The API base is read at build time for the rewrite target and at runtime by
 * the browser client. Both come from the same variable so a misconfigured
 * deploy fails visibly rather than silently proxying to localhost.
 */
const apiUrl = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

const nextConfig: NextConfig = {
  reactStrictMode: true,

  // Standalone output keeps the Docker image small and is what the VPS
  // fallback deployment runs; Vercel ignores it.
  output: 'standalone',

  // The Go API returns its own package identity; transpile the workspace
  // package so Next compiles its TypeScript source directly.
  transpilePackages: ['@meterrail/shared'],

  images: {
    remotePatterns: [
      // Cloudflare R2 public buckets and any custom CDN domain in front.
      { protocol: 'https', hostname: '*.r2.dev' },
      { protocol: 'https', hostname: '*.r2.cloudflarestorage.com' },
      ...(process.env.NEXT_PUBLIC_CDN_HOSTNAME
        ? [{ protocol: 'https' as const, hostname: process.env.NEXT_PUBLIC_CDN_HOSTNAME }]
        : []),
    ],
  },

  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          { key: 'X-Frame-Options', value: 'SAMEORIGIN' },
          {
            key: 'Permissions-Policy',
            // Agora needs camera and microphone; everything else is denied.
            value: 'camera=(self), microphone=(self), geolocation=(), payment=()',
          },
        ],
      },
    ];
  },

  async rewrites() {
    // Same-origin proxy for local development, so the browser never has to
    // deal with CORS or a second cookie domain. In production nginx does this.
    return [
      {
        source: '/api/proxy/:path*',
        destination: `${apiUrl}/:path*`,
      },
    ];
  },
};

export default nextConfig;
