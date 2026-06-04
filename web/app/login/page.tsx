'use client'

import { Button } from '@/components/ui/button'

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080'

export default function LoginPage() {
  const handleGoogleSignIn = () => {
    window.location.href = `${API_URL}/auth/google`
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="w-full max-w-md p-8 bg-white rounded-lg shadow-md space-y-6">
        <div className="text-center">
          <h1 className="text-2xl font-bold text-gray-900">Career Ops</h1>
          <p className="mt-2 text-sm text-gray-600">AI-powered job search pipeline</p>
        </div>
        <Button
          onClick={handleGoogleSignIn}
          className="w-full"
          variant="outline"
        >
          <svg className="w-5 h-5 mr-2" viewBox="0 0 24 24" aria-hidden="true">
            <path
              fill="currentColor"
              d="M12.545 10.239v3.821h5.445c-.712 2.315-2.647 3.972-5.445 3.972a6.033 6.033 0 110-12.064c1.498 0 2.866.549 3.921 1.453l2.814-2.814A9.969 9.969 0 0012.545 2C7.021 2 2.543 6.477 2.543 12s4.478 10 10.002 10c8.396 0 10.249-7.85 9.426-11.748l-9.426-.013z"
            />
          </svg>
          Sign in with Google
        </Button>
      </div>
    </div>
  )
}
