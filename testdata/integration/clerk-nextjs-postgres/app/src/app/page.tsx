import { SignInButton, SignedIn, SignedOut, UserButton } from "@clerk/nextjs";

export default function Home() {
  return (
    <main className="min-h-screen flex flex-col items-center justify-center p-8">
      <div className="max-w-2xl w-full space-y-8">
        <div className="text-center">
          <h1 className="text-4xl font-bold mb-4">
            Clerk + PostgreSQL Integration Test
          </h1>
          <p className="text-gray-600 dark:text-gray-400">
            This app tests cldctl deployment with Clerk authentication and
            PostgreSQL database connectivity.
          </p>
        </div>

        <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6 space-y-4">
          <h2 className="text-xl font-semibold">Authentication Status</h2>

          <SignedOut>
            <div className="space-y-4">
              <p className="text-yellow-600 dark:text-yellow-400">
                You are not signed in.
              </p>
              <SignInButton mode="modal">
                <button className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg transition-colors">
                  Sign In
                </button>
              </SignInButton>
            </div>
          </SignedOut>

          <SignedIn>
            <div className="space-y-4">
              <div className="flex items-center gap-4">
                <p className="text-green-600 dark:text-green-400">
                  You are signed in!
                </p>
                <UserButton />
              </div>
            </div>
          </SignedIn>
        </div>

        <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6 space-y-4">
          <h2 className="text-xl font-semibold">API Endpoints</h2>
          <ul className="space-y-2 text-sm">
            <li>
              <code className="bg-gray-200 dark:bg-gray-700 px-2 py-1 rounded">
                GET /api/health
              </code>
              <span className="ml-2 text-gray-600 dark:text-gray-400">
                - Health check (public)
              </span>
            </li>
            <li>
              <code className="bg-gray-200 dark:bg-gray-700 px-2 py-1 rounded">
                GET /api/protected
              </code>
              <span className="ml-2 text-gray-600 dark:text-gray-400">
                - Protected route (requires auth)
              </span>
            </li>
          </ul>
        </div>

        <div className="text-center text-sm text-gray-500">
          <p>
            Deployed with{" "}
            <a
              href="https://github.com/davidthor/cldctl"
              className="text-blue-600 hover:underline"
            >
              cldctl
            </a>
          </p>
        </div>
      </div>
    </main>
  );
}
