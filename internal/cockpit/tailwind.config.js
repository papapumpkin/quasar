module.exports = {
  content: ["./internal/cockpit/views/**/*.templ"],
  theme: { extend: {
    fontFamily: { mono: ["ui-monospace","SFMono-Regular","Menlo","monospace"] },
    colors: { cyan:{DEFAULT:"#22d3ee"}, green:{DEFAULT:"#34d399"}, amber:{DEFAULT:"#fbbf24"},
              red:{DEFAULT:"#f87171"}, violet:{DEFAULT:"#a78bfa"} },
  } },
};
