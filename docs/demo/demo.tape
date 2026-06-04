# VHS tape for the fan demo GIF. Render with: sh docs/demo/render.sh
# (render.sh builds fan, seeds a scratch git repo, and puts a fake "claude" on
# PATH so this is reproducible and never calls a real agent.)
Output demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1280
Set Height 620
Set Padding 26
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Sleep 600ms
Type `fan run --each 'src/Button.tsx,src/Modal.tsx,src/Tabs.tsx,src/Card.tsx' --agent claude --gate 'true' 'Migrate {} to the new Button API'`
Sleep 700ms
Enter
Sleep 4s
Sleep 2.5s
