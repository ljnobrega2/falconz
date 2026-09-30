/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#fff7ed', 100: '#ffedd5', 200: '#fed7aa', 300: '#fdba74',
          400: '#fb923c', 500: '#f97316', 600: '#ea580c', 700: '#c2410c',
          800: '#9a3412', 900: '#7c2d12',
        },
        // Identidade FALK LOG — azul institucional / grafite / preto
        falk: {
          blue: '#1E6FF2', bluedark: '#1556CC', bluesoft: '#EAF1FE',
          sky: '#5fa0ff', graphite: '#161b26', graphite2: '#1f2633',
          ink: '#0c0f16', steel: '#8b93a3', line: '#2a3343',
        },
      },
      keyframes: {
        'falk-float': { '0%,100%': { transform: 'translateY(0)' }, '50%': { transform: 'translateY(-6px)' } },
      },
      animation: {
        'falk-float': 'falk-float 5s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
