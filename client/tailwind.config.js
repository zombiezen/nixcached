const colors = require('tailwindcss/colors');

/** @type {import('tailwindcss').Config} */
module.exports = {
  content: {
    relative: true,
    files: ["./templates/**/*.html"],
  },
  theme: {
    colors: {
      transparent: colors.transparent,
      current: colors.current,
      black: colors.black,
      white: colors.white,

      gray: colors.gray,
      blue: colors.blue,
      purple: colors.purple,
    },
    extend: {},
  },
  plugins: [],
}
