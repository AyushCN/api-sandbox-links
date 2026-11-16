import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'standalone',
  allowedDevOrigins: [
    '10.10.10.198',
    '192.168.132.215',
    'localhost',
    '127.0.0.1',
  ],
  async rewrites() {
    const backend =
      (process.env.BACKEND_URL ||
        process.env.NEXT_PUBLIC_API_BASE ||
        'http://localhost:8080').trim();
    return {
      beforeFiles: [],
      afterFiles: [],
      fallback: [
        {
          source: '/api/user/me',
          destination: `${backend}/api/me`,
        },
        {
          source: '/api/environments/:id/files',
          destination: `${backend}/api/sandboxes/:id/files`,
        },
        {
          source: '/api/environments/:id/file',
          destination: `${backend}/api/sandboxes/:id/file`,
        },
        {
          source: '/api/environments/:id/files/delete',
          destination: `${backend}/api/sandboxes/:id/files/delete`,
        },
        {
          source: '/api/environments/:id/env',
          destination: `${backend}/api/sandboxes/:id/env`,
        },
        {
          source: '/api/environments/:id/commit',
          destination: `${backend}/api/sandboxes/:id/commit`,
        },
        {
          source: '/api/environments/:id/push',
          destination: `${backend}/api/sandboxes/:id/push`,
        },
        {
          source: '/api/environments/:id/fork',
          destination: `${backend}/api/sandboxes/:id/fork`,
        },
        {
          source: '/api/environments/:id/restart',
          destination: `${backend}/api/sandboxes/:id/deploy`,
        },
        {
          source: '/api/environments/:id/deploy',
          destination: `${backend}/api/sandboxes/:id/deploy`,
        },
        {
          source: '/api/environments/:id',
          destination: `${backend}/api/sandboxes/:id`,
        },
        {
          source: '/api/environments',
          destination: `${backend}/api/sandboxes`,
        },
        {
          source: '/api/:path*',
          destination: `${backend}/api/:path*`,
        },
      ],
    };
  },
};

export default nextConfig;
