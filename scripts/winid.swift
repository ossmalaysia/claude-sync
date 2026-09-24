import CoreGraphics
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
for w in list where (w["kCGWindowOwnerName"] as? String) == "Claude Sync" && (w["kCGWindowLayer"] as? Int) == 0 {
    print(w["kCGWindowNumber"]!)
    break
}
