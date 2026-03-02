import sys

with open('cmd/nebula_apply.go', 'r') as f:
    lines = f.readlines()

# Find the line with "prog := tuiProgram" around line 336
target_idx = None
for i, line in enumerate(lines):
    if 'prog := tuiProgram' in line and i > 330:
        target_idx = i
        break

if target_idx is None:
    print("Could not find target line", file=sys.stderr)
    sys.exit(1)

# We need to insert two lines after "wd := workDir" (target_idx + 2)
# Then insert resume info and cleanup inside the goroutine
wd_idx = target_idx + 2  # "wd := workDir"

# Verify
assert 'wd := workDir' in lines[wd_idx], f"Expected wd line at {wd_idx}, got: {lines[wd_idx]}"

# Insert cpDir and isResume after wd line
insert_after_wd = [
    '\t\t\tcpDir := dir // checkpoint directory for this nebula\n',
    '\t\t\tisResume := resume\n',
]
lines = lines[:wd_idx+1] + insert_after_wd + lines[wd_idx+1:]

# Now find "results, runErr := wg.Run(ctx)" line after the goroutine start
run_idx = None
for i in range(wd_idx + 2, len(lines)):
    if 'results, runErr := wg.Run(ctx)' in lines[i]:
        run_idx = i
        break

assert run_idx is not None, "Could not find wg.Run line"

# Insert resume info check before wg.Run
resume_check = [
    '\t\t\t\tif isResume {\n',
    '\t\t\t\t\tprog.Send(tui.MsgInfo{Msg: fmt.Sprintf("resume mode: found %d checkpoint(s)", checkpointCount)})\n',
    '\t\t\t\t}\n',
]
lines = lines[:run_idx] + resume_check + lines[run_idx:]

# Now find the MsgNebulaDone send (it's after wg.Run)
done_idx = None
for i in range(run_idx + 3, len(lines)):
    if 'prog.Send(tui.MsgNebulaDone' in lines[i]:
        done_idx = i
        break

assert done_idx is not None, "Could not find MsgNebulaDone line"

# Insert checkpoint cleanup before MsgNebulaDone
cleanup = [
    '\t\t\t\t// Clean up checkpoint files on success to prevent stale data.\n',
    '\t\t\t\tif runErr == nil && cpDir != "" {\n',
    '\t\t\t\t\tcleanupCheckpoints(cpDir)\n',
    '\t\t\t\t}\n',
]
lines = lines[:done_idx] + cleanup + lines[done_idx:]

with open('cmd/nebula_apply.go', 'w') as f:
    f.writelines(lines)

print("OK")
