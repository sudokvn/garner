# Files in the library

garner manages one file per episode: the files it imports and the files it takes over. It leaves every other file in the library alone.

## What garner changes

- garner moves downloads into `library.tv` and `library.anime` with a rename, never a copy, and creates the show and season folders there. It never creates the library folders themselves.
- It never overwrites a file it did not import or take over. An import that would do so stops until you move that file.
- After an import, it deletes the rest of the download from SABnzbd's completed folder, unless the folder still holds videos it could not assign to an episode. After the file check rejects a download, it deletes the download's folder. It deletes a whole folder only when the folder is named after the release and holds no file garner manages.
- When an upgrade replaces a file, garner deletes the old file with its subtitle files.
- Deleting a show with **Delete files** deletes the files garner manages for it, with their subtitle files. **Keep files** leaves them in place.
- It removes the folders that its moves and deletions leave empty, but never a library folder or one of Jellyfin's library folders.

## Taking over files

When you add a show, garner looks for the show's video files before the monitor starts on it. It looks in two places:

- garner's folder for the show, such as `Harbor.Precinct.Night.Shift.2013.[tvdbid-42]`
- with Jellyfin, every folder that Jellyfin holds the show in, found by the show's TVDB ID, whatever the folder is called

garner takes over a file when its name clearly names episodes that have no file yet. FFprobe reads the file's audio languages and resolution to find the profile preference the file meets, as for a download. A file that meets no preference, such as English audio under the German profile, ranks below all of them, so an upgrade replaces it if the profile allows upgrades. A file whose audio language garner cannot tell is never upgraded. Of two files for one episode, garner takes the one that meets the better preference, then the larger one.

The terminal interface lists the files that garner left alone, with the reason. To look again, for example after copying files into the library by hand, press `t` on the show's details page.

If Jellyfin sees its folders under other paths than garner does, map them with `jellyfin.paths` in `garner.toml`. Map every folder that Jellyfin's libraries use, including the folders of a library from before garner, and mount each one into garner's container at the mapped path. Once `jellyfin.paths` is set, garner skips a folder that no entry covers, and the terminal interface names it.

## What taking over means for your files

A taken-over file belongs to garner from then on, like a file it imported: an upgrade can replace it, and **Delete files** deletes it. After **Keep files**, adding the show again takes the file over again.

By default, the file keeps its name and folder. After a take-over, the terminal interface asks whether to rename and move the files outside garner's folder into that folder, named the way garner names files. **Keep as they are** is preselected.

Renaming moves the subtitle files along, never overwrites a file and, if you use Jellyfin, tells it about the moves. It removes the folders that the moves left empty. A folder that still holds other files, such as Jellyfin's `tvshow.nfo`, stays, and Jellyfin may still list it as a show. garner moves files without copying them, so a file on another filesystem than garner's folder stays where it is.
